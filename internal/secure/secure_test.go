package secure

import (
	"testing"
)

func TestKeyGenAndCrypto(t *testing.T) {
	// Generate two keypairs
	kpA, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	kpB, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Derive shared secrets (should be identical from both sides)
	sharedA, err := ComputeSharedSecret(kpA.PrivateKey, kpB.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sharedB, err := ComputeSharedSecret(kpB.PrivateKey, kpA.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if sharedA != sharedB {
		t.Fatal("shared secrets don't match")
	}

	// Derive encryption keys
	keyA, err := DeriveEncryptionKey(sharedA)
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := DeriveEncryptionKey(sharedB)
	if err != nil {
		t.Fatal(err)
	}

	// Encrypt with A's key, decrypt with B's key
	msg := "hello secure world!"
	encrypted, err := Encrypt(keyA, msg)
	if err != nil {
		t.Fatal(err)
	}

	if !IsEncryptedMessage(encrypted) {
		t.Fatal("encrypted message not detected")
	}

	decrypted, err := Decrypt(keyB, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != msg {
		t.Fatalf("decryption mismatch: got %q want %q", decrypted, msg)
	}

	// Session manager test
	smA := NewSessionManager(kpA)
	smB := NewSessionManager(kpB)

	sessA, err := smA.GetOrCreateSession("userB", kpB.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sessB, err := smB.GetOrCreateSession("userA", kpA.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = sessA
	_ = sessB

	enc, err := smA.EncryptMessage("userB", "secret message")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := smB.DecryptMessage("userA", enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != "secret message" {
		t.Fatalf("session decrypt mismatch: got %q", dec)
	}

	t.Log("Fingerprint A:", kpA.Fingerprint())
	t.Log("Fingerprint B:", kpB.Fingerprint())
	t.Log("All crypto tests passed")
}
