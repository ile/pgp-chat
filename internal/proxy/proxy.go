// Package proxy pairs two clients by room and forwards opaque OpenPGP frames.
package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sync"
	"time"
)

const MaxFrameSize = 4 << 20
const maxRooms = 1024
const waitTimeout = 5 * time.Minute

var roomPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
var errRetry = errors.New("stale room partner; retry")

// Conn carries the same framed ciphertext as the direct transport.
type Conn struct {
	conn net.Conn
	room string
	mu   sync.Mutex
}

func (c *Conn) PeerLabel() string      { return "room " + c.room }
func (c *Conn) TransportLabel() string { return "TCP proxy / OpenPGP" }
func (c *Conn) Close() error           { return c.conn.Close() }

func (c *Conn) Send(payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxFrameSize {
		return fmt.Errorf("invalid message size: %d", len(payload))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeAll(c.conn, header[:]); err != nil {
		return err
	}
	return writeAll(c.conn, payload)
}

func (c *Conn) Receive() ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(c.conn, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxFrameSize {
		return nil, fmt.Errorf("invalid message frame size: %d", size)
	}
	payload := make([]byte, size)
	_, err := io.ReadFull(c.conn, payload)
	return payload, err
}

// Join waits until a second client joins the same room.
func Join(ctx context.Context, address, room string) (*Conn, error) {
	if !roomPattern.MatchString(room) {
		return nil, errors.New("room code must be 8–64 letters, digits, underscores or hyphens")
	}
	if address == "" {
		return nil, errors.New("proxy address is required")
	}
	for attempt := 0; attempt < 3; attempt++ {
		conn, err := joinOnce(ctx, address, room)
		if !errors.Is(err, errRetry) {
			return conn, err
		}
	}
	return nil, errors.New("proxy room changed repeatedly; try again")
}

func joinOnce(ctx context.Context, address, room string) (*Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	if err = writeAll(conn, append([]byte{byte(len(room))}, room...)); err != nil {
		return nil, fmt.Errorf("send room code: %w", err)
	}
	var response [1]byte
	if _, err = io.ReadFull(conn, response[:]); err != nil {
		return nil, fmt.Errorf("wait for peer: %w", err)
	}
	if response[0] != 1 {
		if response[0] == 2 {
			err = errRetry
			return nil, err
		}
		err = errors.New("proxy rejected room (already full or server busy)")
		return nil, err
	}
	return &Conn{conn: conn, room: room}, nil
}

// Serve accepts client connections until ctx is cancelled. The listener is
// supplied by the caller so deployment and tests can choose their own address.
func Serve(ctx context.Context, listener net.Listener) error {
	type waiting struct {
		conn     net.Conn
		timer    *time.Timer
		done     chan struct{}
		readDone chan struct{}
		readErr  error
	}
	var mu sync.Mutex
	rooms := make(map[string]*waiting)
	active := make(map[string]struct{})
	handshakes := make(chan struct{}, maxRooms)
	var all sync.Map
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		mu.Lock()
		for room, entry := range rooms {
			entry.timer.Stop()
			_ = entry.conn.Close()
			close(entry.done)
			delete(rooms, room)
		}
		mu.Unlock()
		all.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case handshakes <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		all.Store(conn, struct{}{})
		go func(conn net.Conn) {
			defer all.Delete(conn)
			handshakeHeld := true
			defer func() {
				if handshakeHeld {
					<-handshakes
				}
			}()
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			var length [1]byte
			if _, err := io.ReadFull(conn, length[:]); err != nil || length[0] < 8 || length[0] > 64 {
				_ = conn.Close()
				return
			}
			code := make([]byte, length[0])
			if _, err := io.ReadFull(conn, code); err != nil || !roomPattern.Match(code) {
				_ = conn.Close()
				return
			}
			_ = conn.SetReadDeadline(time.Time{})
			<-handshakes
			handshakeHeld = false
			room := string(code)
			mu.Lock()
			if ctx.Err() != nil {
				mu.Unlock()
				_ = conn.Close()
				return
			}
			_, busy := active[room]
			if busy || (rooms[room] == nil && len(rooms)+len(active) >= maxRooms) {
				mu.Unlock()
				_, _ = conn.Write([]byte{0})
				_ = conn.Close()
				return
			}
			if first := rooms[room]; first != nil {
				delete(rooms, room)
				first.timer.Stop()
				active[room] = struct{}{}
				mu.Unlock()
				// Stop the pending disconnect probe before forwarding any bytes.
				_ = first.conn.SetReadDeadline(time.Now())
				<-first.readDone
				_ = first.conn.SetReadDeadline(time.Time{})
				if first.readErr == nil || !isTimeout(first.readErr) {
					_ = first.conn.Close()
					_, _ = conn.Write([]byte{2})
					_ = conn.Close()
					close(first.done)
					mu.Lock()
					delete(active, room)
					mu.Unlock()
					return
				}
				if _, err := first.conn.Write([]byte{1}); err != nil {
					_ = first.conn.Close()
					_ = conn.Close()
					close(first.done)
					mu.Lock()
					delete(active, room)
					mu.Unlock()
					return
				}
				if _, err := conn.Write([]byte{1}); err != nil {
					_ = first.conn.Close()
					_ = conn.Close()
					close(first.done)
					mu.Lock()
					delete(active, room)
					mu.Unlock()
					return
				}
				go func() { _, _ = io.Copy(first.conn, conn); _ = first.conn.Close() }()
				_, _ = io.Copy(conn, first.conn)
				_ = conn.Close()
				_ = first.conn.Close()
				close(first.done)
				mu.Lock()
				delete(active, room)
				mu.Unlock()
				return
			}
			entry := &waiting{conn: conn, done: make(chan struct{}), readDone: make(chan struct{})}
			entry.timer = time.AfterFunc(waitTimeout, func() {
				mu.Lock()
				if rooms[room] == entry {
					delete(rooms, room)
					_ = conn.Close()
					close(entry.done)
				}
				mu.Unlock()
			})
			rooms[room] = entry
			mu.Unlock()
			var probe [1]byte
			_, entry.readErr = conn.Read(probe[:]) // clients send nothing until paired
			close(entry.readDone)
			mu.Lock()
			pending := rooms[room] == entry
			if pending {
				delete(rooms, room)
				entry.timer.Stop()
				close(entry.done)
			}
			mu.Unlock()
			if pending {
				_ = conn.Close()
				return
			}
			<-entry.done
		}(conn)
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
