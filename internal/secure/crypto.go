package secure

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"crypto/sha256"
	"io"
)

const (
	// MessagePrefix marks an E2E encrypted message in Slack.
	MessagePrefix = "[SLACKERS_E2E:"
	MessageSuffix = "]"
)

// DeriveEncryptionKey derives a symmetric key from a shared secret using HKDF.
func DeriveEncryptionKey(sharedSecret [32]byte) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, sharedSecret[:], nil, []byte("slackers-e2e-v1"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("deriving key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext with ChaCha20-Poly1305 and returns the formatted message.
func Encrypt(key []byte, plaintext string) (string, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)

	// Format: [SLACKERS_E2E:<base64 nonce>:<base64 ciphertext>]
	nonceB64 := base64.StdEncoding.EncodeToString(nonce)
	ctB64 := base64.StdEncoding.EncodeToString(ciphertext)

	return fmt.Sprintf("%s%s:%s%s", MessagePrefix, nonceB64, ctB64, MessageSuffix), nil
}

// Decrypt decrypts a formatted E2E message.
func Decrypt(key []byte, message string) (string, error) {
	// Strip prefix and suffix.
	if !strings.HasPrefix(message, MessagePrefix) || !strings.HasSuffix(message, MessageSuffix) {
		return "", fmt.Errorf("not an E2E message")
	}
	inner := message[len(MessagePrefix) : len(message)-len(MessageSuffix)]

	parts := strings.SplitN(inner, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid E2E format")
	}

	nonce, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decoding nonce: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decoding ciphertext: %w", err)
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}

	return string(plaintext), nil
}

// IsEncryptedMessage checks if a message is E2E encrypted.
func IsEncryptedMessage(text string) bool {
	return strings.HasPrefix(text, MessagePrefix) && strings.HasSuffix(text, MessageSuffix)
}

// suppress unused import warning
var _ = binary.BigEndian
