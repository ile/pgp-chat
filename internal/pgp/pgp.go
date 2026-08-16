package pgp

import (
	"errors"
	"fmt"
	"os"

	openpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
)

// Identity contains the local private key and the peer's public key.
// Private key material must stay on the local machine.
type Identity struct {
	Private *openpgp.Key
	Public  *openpgp.Key
	Peer    *openpgp.Key
}

func GenerateKey(name, email string) (*openpgp.Key, error) {
	return openpgp.PGP().
		KeyGeneration().
		AddUserId(name, email).
		OverrideProfileAlgorithm(openpgp.KeyGenerationCurve25519).
		New().
		GenerateKey()
}

func SaveKeyPair(key *openpgp.Key, privatePath, publicPath string, passphrase []byte) error {
	privateKey := key
	var err error
	if len(passphrase) > 0 {
		privateKey, err = openpgp.PGP().LockKey(key, passphrase)
		if err != nil {
			return fmt.Errorf("lock private key: %w", err)
		}
	}

	privateArmored, err := privateKey.Armor()
	if err != nil {
		return fmt.Errorf("armor private key: %w", err)
	}
	publicArmored, err := key.GetArmoredPublicKey()
	if err != nil {
		return fmt.Errorf("armor public key: %w", err)
	}

	if err := writePrivateFile(privatePath, []byte(privateArmored)); err != nil {
		return err
	}
	if err := os.WriteFile(publicPath, []byte(publicArmored), 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}
	return nil
}

func LoadIdentity(privatePath, peerPath, passphrasePath string) (*Identity, error) {
	privateData, err := os.ReadFile(privatePath)
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", privatePath, err)
	}
	privateKey, err := openpgp.NewKey(privateData)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	if !privateKey.IsPrivate() {
		return nil, errors.New("the local key file does not contain a private key")
	}
	locked, err := privateKey.IsLocked()
	if err != nil {
		return nil, fmt.Errorf("inspect private key: %w", err)
	}
	if locked {
		if passphrasePath == "" {
			return nil, errors.New("private key is locked; provide --passphrase-file")
		}
		passphrase, err := os.ReadFile(passphrasePath)
		if err != nil {
			return nil, fmt.Errorf("read passphrase file: %w", err)
		}
		privateKey, err = privateKey.Unlock(trimTrailingNewline(passphrase))
		if err != nil {
			return nil, fmt.Errorf("unlock private key: %w", err)
		}
	}

	publicKey, err := publicOnly(privateKey)
	if err != nil {
		return nil, err
	}
	peerKey, err := LoadPublicKey(peerPath)
	if err != nil {
		return nil, err
	}
	return &Identity{Private: privateKey, Public: publicKey, Peer: peerKey}, nil
}

func LoadPublicKey(path string) (*openpgp.Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read peer public key %q: %w", path, err)
	}
	key, err := openpgp.NewKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse peer public key: %w", err)
	}
	if key.IsPrivate() {
		return publicOnly(key)
	}
	return key, nil
}

func EncryptAndSign(identity *Identity, plaintext []byte) ([]byte, error) {
	if identity == nil || identity.Private == nil || identity.Peer == nil {
		return nil, errors.New("incomplete PGP identity")
	}
	signer, err := openpgp.NewKeyRing(identity.Private)
	if err != nil {
		return nil, fmt.Errorf("create signing keyring: %w", err)
	}
	recipient, err := openpgp.NewKeyRing(identity.Peer)
	if err != nil {
		return nil, fmt.Errorf("create recipient keyring: %w", err)
	}
	handle, err := openpgp.PGP().Encryption().
		Recipients(recipient).
		SigningKeys(signer).
		New()
	if err != nil {
		return nil, fmt.Errorf("create encryption handle: %w", err)
	}
	message, err := handle.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt message: %w", err)
	}
	return message.Bytes(), nil
}

func DecryptAndVerify(identity *Identity, ciphertext []byte) ([]byte, error) {
	if identity == nil || identity.Private == nil || identity.Peer == nil {
		return nil, errors.New("incomplete PGP identity")
	}
	verifier, err := openpgp.NewKeyRing(identity.Peer)
	if err != nil {
		return nil, fmt.Errorf("create verification keyring: %w", err)
	}
	handle, err := openpgp.PGP().Decryption().
		DecryptionKey(identity.Private).
		VerificationKeys(verifier).
		MaxDecompressedMessageSize(4 << 20).
		New()
	if err != nil {
		return nil, fmt.Errorf("create decryption handle: %w", err)
	}
	result, err := handle.Decrypt(ciphertext, openpgp.Bytes)
	if err != nil {
		return nil, fmt.Errorf("decrypt message: %w", err)
	}
	if len(result.Signatures) == 0 {
		return nil, errors.New("message has no OpenPGP signature")
	}
	if err := result.SignatureError(); err != nil {
		return nil, fmt.Errorf("invalid OpenPGP signature: %w", err)
	}
	return result.Bytes(), nil
}

func Fingerprint(key *openpgp.Key) string {
	if key == nil {
		return ""
	}
	return key.GetFingerprint()
}

func writePrivateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	return nil
}

func publicOnly(key *openpgp.Key) (*openpgp.Key, error) {
	armored, err := key.GetArmoredPublicKey()
	if err != nil {
		return nil, fmt.Errorf("extract public key: %w", err)
	}
	publicKey, err := openpgp.NewKeyFromArmored(armored)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	return publicKey, nil
}

func trimTrailingNewline(data []byte) []byte {
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}
	return data
}
