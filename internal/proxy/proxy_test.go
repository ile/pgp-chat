package proxy

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPairAndForward(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, listener) }()
	room := "test-room-123456"
	joinCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	type result struct {
		conn *Conn
		err  error
	}
	first := make(chan result, 1)
	go func() { c, err := Join(joinCtx, listener.Addr().String(), room); first <- result{c, err} }()
	second, err := Join(joinCtx, listener.Addr().String(), room)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	joined := <-first
	if joined.err != nil {
		t.Fatal(joined.err)
	}
	defer joined.conn.Close()
	payload := []byte("opaque ciphertext")
	if err := joined.conn.Send(payload); err != nil {
		t.Fatal(err)
	}
	got, err := second.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q", got)
	}
	if err := second.Send([]byte("reply")); err != nil {
		t.Fatal(err)
	}
	got, err = joined.conn.Receive()
	if err != nil || string(got) != "reply" {
		t.Fatalf("reply %q: %v", got, err)
	}
	_, err = Join(joinCtx, listener.Addr().String(), room)
	if err == nil || !strings.Contains(err.Error(), "already full") {
		t.Fatalf("expected room rejection, got %v", err)
	}
}

func TestInvalidRoom(t *testing.T) {
	_, err := Join(context.Background(), "127.0.0.1:1", "short")
	if err == nil || !strings.Contains(err.Error(), "room code") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitingClientDisconnectReleasesRoom(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	go func() { _ = Serve(ctx, listener) }()
	address := listener.Addr().String()
	room := "unused-room-1234"
	waitingCtx, cancelWaiting := context.WithCancel(ctx)
	waitingDone := make(chan error, 1)
	go func() { _, err := Join(waitingCtx, address, room); waitingDone <- err }()
	// Let the first connection reach the waiting state before cancelling it.
	time.Sleep(20 * time.Millisecond)
	cancelWaiting()
	if err := <-waitingDone; err == nil {
		t.Fatal("cancelled join succeeded")
	}
	joinCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	first := make(chan error, 1)
	release := make(chan struct{})
	go func() {
		c, err := Join(joinCtx, address, room)
		if err == nil {
			defer c.Close()
		}
		first <- err
		if err == nil {
			<-release
		}
	}()
	second, err := Join(joinCtx, address, room)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	close(release)
}
