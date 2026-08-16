package pgp

import (
	"os"
	"path/filepath"
	"testing"
)

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
