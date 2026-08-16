package pgp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
)

func TestLoadIdentityChecksPeerBeforePassphrase(t *testing.T) {
	dir := t.TempDir()
	key, err := GenerateKey("alice", "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(dir, "alice.private.asc")
	if err := SaveKeyPair(key, privatePath, filepath.Join(dir, "alice.public.asc"), []byte("secret")); err != nil {
		t.Fatal(err)
	}

	_, err = LoadIdentity(privatePath, filepath.Join(dir, "missing-peer.asc"), "")
	if err == nil || !strings.Contains(err.Error(), "read peer public key") {
		t.Fatalf("expected missing peer public key error, got %v", err)
	}
}

func TestGenerateKeyWithoutEmail(t *testing.T) {
	key, err := GenerateKey("anonymous", "")
	if err != nil {
		t.Fatal(err)
	}
	keyRing, err := openpgp.NewKeyRing(key)
	if err != nil {
		t.Fatal(err)
	}
	identities := keyRing.GetIdentities()
	if len(identities) != 1 {
		t.Fatalf("expected one identity, got %d", len(identities))
	}
	if identities[0].Name != "anonymous" {
		t.Fatalf("unexpected identity name: %q", identities[0].Name)
	}
	if identities[0].Email != "" {
		t.Fatalf("expected no email, got %q", identities[0].Email)
	}
}

func TestProtectedKeyRequiresPassphrase(t *testing.T) {
	dir := t.TempDir()
	key, err := GenerateKey("alice", "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(dir, "alice.private.asc")
	peerPath := filepath.Join(dir, "peer.public.asc")
	if err := SaveKeyPair(key, privatePath, peerPath, []byte("secret")); err != nil {
		t.Fatal(err)
	}

	_, err = LoadIdentity(privatePath, peerPath, "")
	if !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("expected ErrPassphraseRequired, got %v", err)
	}
}

func TestProtectedKeyPairRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyA, err := GenerateKey("alice", "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := GenerateKey("bob", "bob@example.test")
	if err != nil {
		t.Fatal(err)
	}

	passphrasePath := filepath.Join(dir, "alice.pass")
	if err := os.WriteFile(passphrasePath, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveKeyPair(keyA, filepath.Join(dir, "alice.private.asc"), filepath.Join(dir, "alice.public.asc"), []byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	if err := SaveKeyPair(keyB, filepath.Join(dir, "bob.private.asc"), filepath.Join(dir, "bob.public.asc"), nil); err != nil {
		t.Fatal(err)
	}

	alice, err := LoadIdentity(filepath.Join(dir, "alice.private.asc"), filepath.Join(dir, "bob.public.asc"), passphrasePath)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := LoadIdentity(filepath.Join(dir, "bob.private.asc"), filepath.Join(dir, "alice.public.asc"), "")
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := EncryptAndSign(alice, []byte("protected round trip"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := DecryptAndVerify(bob, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "protected round trip" {
		t.Fatalf("unexpected plaintext: %q", plaintext)
	}
}
