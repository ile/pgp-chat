package chat

import (
	"context"
	"testing"
	"time"

	"github.com/nigel-dev/pgp-chat/internal/p2p"
	"github.com/nigel-dev/pgp-chat/internal/pgp"
)

func TestSessionEncryptsAndVerifiesAcrossLibp2p(t *testing.T) {
	keyA, err := pgp.GenerateKey("alice", "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := pgp.GenerateKey("bob", "bob@example.test")
	if err != nil {
		t.Fatal(err)
	}

	nodeA, err := p2p.NewNode(p2p.Config{ListenAddr: "/ip4/127.0.0.1/tcp/0", NodeKey: t.TempDir() + "/a.node.key"})
	if err != nil {
		t.Fatal(err)
	}
	defer nodeA.Close()
	nodeB, err := p2p.NewNode(p2p.Config{ListenAddr: "/ip4/127.0.0.1/tcp/0", NodeKey: t.TempDir() + "/b.node.key"})
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Close()
	t.Logf("node A: %v", nodeA.Addresses())
	t.Logf("node B: %v", nodeB.Addresses())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	accepted, cleanup, err := nodeB.PrepareAccept("")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	connA, _, err := nodeA.Dial(ctx, nodeB.Addresses()[0])
	if err != nil {
		t.Fatal(err)
	}
	defer connA.Close()

	var connB *p2p.Conn
	select {
	case connB = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer connB.Close()

	sessionA := NewSession(connA, &pgp.Identity{Private: keyA, Public: keyA, Peer: keyB})
	sessionB := NewSession(connB, &pgp.Identity{Private: keyB, Public: keyB, Peer: keyA})
	if err := sessionA.Send("hello over libp2p"); err != nil {
		t.Fatal(err)
	}
	message, err := sessionB.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if message.Body != "hello over libp2p" {
		t.Fatalf("unexpected message body: %q", message.Body)
	}
	if message.SenderFingerprint != pgp.Fingerprint(keyA) {
		t.Fatalf("unexpected sender fingerprint: %s", message.SenderFingerprint)
	}
}
