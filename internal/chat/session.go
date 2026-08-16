package chat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nigel-dev/pgp-chat/internal/p2p"
	"github.com/nigel-dev/pgp-chat/internal/pgp"
)

const protocolVersion = 1

type Envelope struct {
	Version           int       `json:"version"`
	ID                string    `json:"id"`
	Sequence          uint64    `json:"sequence"`
	SentAt            time.Time `json:"sent_at"`
	SenderFingerprint string    `json:"sender_fingerprint"`
	Body              string    `json:"body"`
}

type Event struct {
	Message Envelope
	Err     error
}

type Session struct {
	conn     *p2p.Conn
	identity *pgp.Identity

	sequenceMu sync.Mutex
	sequence   uint64
	seenMu     sync.Mutex
	seen       map[string]struct{}
}

func NewSession(conn *p2p.Conn, identity *pgp.Identity) *Session {
	return &Session{
		conn:     conn,
		identity: identity,
		seen:     make(map[string]struct{}),
	}
}

func (s *Session) Send(body string) error {
	if body == "" {
		return fmt.Errorf("cannot send an empty message")
	}
	s.sequenceMu.Lock()
	s.sequence++
	sequence := s.sequence
	s.sequenceMu.Unlock()

	id, err := newMessageID()
	if err != nil {
		return fmt.Errorf("create message ID: %w", err)
	}
	envelope := Envelope{
		Version:           protocolVersion,
		ID:                id,
		Sequence:          sequence,
		SentAt:            time.Now().UTC(),
		SenderFingerprint: pgp.Fingerprint(s.identity.Public),
		Body:              body,
	}
	plaintext, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	ciphertext, err := pgp.EncryptAndSign(s.identity, plaintext)
	if err != nil {
		return err
	}
	return s.conn.Send(ciphertext)
}

func (s *Session) Receive() (Envelope, error) {
	ciphertext, err := s.conn.Receive()
	if err != nil {
		return Envelope{}, err
	}
	plaintext, err := pgp.DecryptAndVerify(s.identity, ciphertext)
	if err != nil {
		return Envelope{}, err
	}
	var envelope Envelope
	if err := json.Unmarshal(plaintext, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode message envelope: %w", err)
	}
	if envelope.Version != protocolVersion {
		return Envelope{}, fmt.Errorf("unsupported message version: %d", envelope.Version)
	}
	if envelope.ID == "" || envelope.Body == "" {
		return Envelope{}, fmt.Errorf("invalid message envelope")
	}
	if envelope.SenderFingerprint != pgp.Fingerprint(s.identity.Peer) {
		return Envelope{}, fmt.Errorf("message sender fingerprint does not match configured peer")
	}
	if s.markSeen(envelope.ID) {
		return Envelope{}, fmt.Errorf("duplicate message rejected: %s", envelope.ID)
	}
	return envelope, nil
}

func (s *Session) Events() <-chan Event {
	events := make(chan Event, 8)
	go func() {
		defer close(events)
		for {
			message, err := s.Receive()
			events <- Event{Message: message, Err: err}
			if err != nil {
				return
			}
		}
	}()
	return events
}

func (s *Session) PeerFingerprint() string {
	return pgp.Fingerprint(s.identity.Peer)
}

func (s *Session) LocalFingerprint() string {
	return pgp.Fingerprint(s.identity.Public)
}

func (s *Session) RemotePeer() string {
	return s.conn.RemotePeer().String()
}

func (s *Session) Close() error {
	return s.conn.Close()
}

func (s *Session) markSeen(id string) bool {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if _, exists := s.seen[id]; exists {
		return true
	}
	s.seen[id] = struct{}{}
	return false
}

func newMessageID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
