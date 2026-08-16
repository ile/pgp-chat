package p2p

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	libcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	libp2pnoise "github.com/libp2p/go-libp2p/p2p/security/noise"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	ProtocolID   protocol.ID = "/pgp-chat/1.0.0"
	MaxFrameSize             = 4 << 20
)

var streamHandshake = []byte("PGP-CHAT-STREAM-1\x00")

type Config struct {
	ListenAddr string
	NodeKey    string
	Relays     []string
}

type Node struct {
	host host.Host
}

type Conn struct {
	stream  network.Stream
	writeMu sync.Mutex
}

func NewNode(cfg Config) (*Node, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "/ip4/0.0.0.0/tcp/0"
	}
	privateKey, err := loadOrCreateNodeKey(cfg.NodeKey)
	if err != nil {
		return nil, err
	}

	opts := []libp2p.Option{
		libp2p.Identity(privateKey),
		libp2p.ListenAddrStrings(cfg.ListenAddr),
		libp2p.Security(string(libp2pnoise.ID), libp2pnoise.New),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
	}
	if len(cfg.Relays) > 0 {
		relays, err := parseRelays(cfg.Relays)
		if err != nil {
			return nil, err
		}
		opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(relays))
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	return &Node{host: h}, nil
}

func (n *Node) ID() peer.ID {
	return n.host.ID()
}

func (n *Node) Addresses() []string {
	addresses := make([]string, 0, len(n.host.Addrs()))
	peerSuffix, _ := ma.NewMultiaddr("/p2p/" + n.ID().String())
	for _, address := range n.host.Addrs() {
		addresses = append(addresses, address.Encapsulate(peerSuffix).String())
	}
	return addresses
}

func (n *Node) Close() error {
	return n.host.Close()
}

func (n *Node) Accept(ctx context.Context, expectedPeer string) (*Conn, error) {
	accepted, cleanup, err := n.PrepareAccept(expectedPeer)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	select {
	case conn := <-accepted:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// PrepareAccept installs the stream handler before the caller starts dialing.
// The cleanup function removes the handler and should be called when the
// caller no longer accepts new sessions.
func (n *Node) PrepareAccept(expectedPeer string) (<-chan *Conn, func(), error) {
	accepted := make(chan *Conn, 1)
	expected, err := parsePeerID(expectedPeer)
	if err != nil {
		return nil, nil, err
	}

	n.host.SetStreamHandler(ProtocolID, func(stream network.Stream) {
		if expected != "" && stream.Conn().RemotePeer() != expected {
			_ = stream.Reset()
			return
		}
		_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
		if !readHandshake(stream) {
			_ = stream.Reset()
			return
		}
		_ = stream.SetReadDeadline(time.Time{})
		select {
		case accepted <- &Conn{stream: stream}:
		default:
			_ = stream.Reset()
		}
	})
	return accepted, func() { n.host.RemoveStreamHandler(ProtocolID) }, nil
}

func (n *Node) Dial(ctx context.Context, address string) (*Conn, peer.ID, error) {
	multiaddr, err := ma.NewMultiaddr(address)
	if err != nil {
		return nil, "", fmt.Errorf("parse peer address: %w", err)
	}
	info, err := peer.AddrInfoFromP2pAddr(multiaddr)
	if err != nil {
		return nil, "", fmt.Errorf("parse peer address info: %w", err)
	}
	if err := n.host.Connect(ctx, *info); err != nil {
		return nil, "", fmt.Errorf("connect to peer: %w", err)
	}
	stream, err := n.host.NewStream(ctx, info.ID, ProtocolID)
	if err != nil {
		return nil, "", fmt.Errorf("open pgp-chat stream: %w", err)
	}
	if err := writeAll(stream, streamHandshake); err != nil {
		_ = stream.Reset()
		return nil, "", fmt.Errorf("send stream handshake: %w", err)
	}
	return &Conn{stream: stream}, info.ID, nil
}

func (c *Conn) RemotePeer() peer.ID {
	return c.stream.Conn().RemotePeer()
}

func (c *Conn) Send(payload []byte) error {
	if len(payload) == 0 {
		return errors.New("cannot send an empty message")
	}
	if len(payload) > MaxFrameSize {
		return fmt.Errorf("message exceeds maximum size of %d bytes", MaxFrameSize)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeAll(c.stream, header[:]); err != nil {
		return err
	}
	return writeAll(c.stream, payload)
}

func (c *Conn) Receive() ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(c.stream, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MaxFrameSize {
		return nil, fmt.Errorf("invalid message frame size: %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.stream, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *Conn) Close() error {
	return c.stream.Close()
}

func readHandshake(reader io.Reader) bool {
	got := make([]byte, len(streamHandshake))
	if _, err := io.ReadFull(reader, got); err != nil {
		return false
	}
	return string(got) == string(streamHandshake)
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}

func loadOrCreateNodeKey(path string) (libcrypto.PrivKey, error) {
	if path == "" {
		return nil, errors.New("node key path is empty")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		key, err := libcrypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse libp2p node key: %w", err)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read libp2p node key: %w", err)
	}

	key, _, err := libcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate libp2p node key: %w", err)
	}
	data, err = libcrypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("serialize libp2p node key: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write libp2p node key: %w", err)
	}
	return key, nil
}

func parseRelays(addresses []string) ([]peer.AddrInfo, error) {
	relays := make([]peer.AddrInfo, 0, len(addresses))
	for _, raw := range addresses {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		address, err := ma.NewMultiaddr(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("parse relay address %q: %w", raw, err)
		}
		info, err := peer.AddrInfoFromP2pAddr(address)
		if err != nil {
			return nil, fmt.Errorf("parse relay peer info %q: %w", raw, err)
		}
		relays = append(relays, *info)
	}
	return relays, nil
}

func parsePeerID(raw string) (peer.ID, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	id, err := peer.Decode(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse expected peer ID: %w", err)
	}
	return id, nil
}
