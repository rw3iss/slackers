// Package secure provides end-to-end encrypted P2P messaging between Slackers users.
package secure

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/curve25519"
)

// KeyPair holds an X25519 keypair for key exchange.
type KeyPair struct {
	PrivateKey [32]byte
	PublicKey  [32]byte
}

// GenerateKeyPair creates a new X25519 keypair.
func GenerateKeyPair() (*KeyPair, error) {
	var kp KeyPair

	// Generate random private key.
	if _, err := rand.Read(kp.PrivateKey[:]); err != nil {
		return nil, fmt.Errorf("generating private key: %w", err)
	}

	// Clamp private key per X25519 spec.
	kp.PrivateKey[0] &= 248
	kp.PrivateKey[31] &= 127
	kp.PrivateKey[31] |= 64

	// Derive public key.
	pub, err := curve25519.X25519(kp.PrivateKey[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("deriving public key: %w", err)
	}
	copy(kp.PublicKey[:], pub)

	return &kp, nil
}

// PublicKeyBase64 returns the public key as a base64 string.
func (kp *KeyPair) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PublicKey[:])
}

// Fingerprint returns a human-readable fingerprint of the public key.
func (kp *KeyPair) Fingerprint() string {
	b := kp.PublicKey[:]
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X:%02X:%02X",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7])
}

// SavePrivateKey writes the private key to a file with 0600 permissions.
func (kp *KeyPair) SavePrivateKey(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(kp.PrivateKey[:])
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}
	return nil
}

// LoadKeyPair loads a keypair from a private key file. Derives the public key.
func LoadKeyPair(path string) (*KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading private key: %w", err)
	}

	privBytes, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("decoding private key: %w", err)
	}
	if len(privBytes) != 32 {
		return nil, fmt.Errorf("invalid private key length: %d", len(privBytes))
	}

	var kp KeyPair
	copy(kp.PrivateKey[:], privBytes)

	pub, err := curve25519.X25519(kp.PrivateKey[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("deriving public key: %w", err)
	}
	copy(kp.PublicKey[:], pub)

	return &kp, nil
}

// DefaultKeyPath returns the default path for the private key file.
func DefaultKeyPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "secure.key")
}

// KeyExists checks if a private key file exists.
func KeyExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// PublicKeyFromBase64 decodes a base64-encoded public key.
func PublicKeyFromBase64(s string) ([32]byte, error) {
	var key [32]byte
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return key, fmt.Errorf("decoding public key: %w", err)
	}
	if len(b) != 32 {
		return key, fmt.Errorf("invalid public key length: %d", len(b))
	}
	copy(key[:], b)
	return key, nil
}

// ComputeSharedSecret derives a shared secret from own private key and peer's public key.
func ComputeSharedSecret(privateKey [32]byte, peerPublicKey [32]byte) ([32]byte, error) {
	var shared [32]byte
	result, err := curve25519.X25519(privateKey[:], peerPublicKey[:])
	if err != nil {
		return shared, fmt.Errorf("computing shared secret: %w", err)
	}
	copy(shared[:], result)
	return shared, nil
}
