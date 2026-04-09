package friends

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A minimal valid contact card for round-trip tests. The public key
// must be exactly 32 raw bytes (base64-encoded). The multiaddr has
// to be IPv4/TCP/p2p shape because that's what EncodeContactCard
// parses.
func fixtureCard() ContactCard {
	pub := make([]byte, 32)
	for i := range pub {
		pub[i] = byte(i)
	}
	return ContactCard{
		Version:   2,
		SlackerID: "test",
		Name:      "Test",
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		// A real libp2p peer ID in base58btc form. This one was
		// generated from a known ed25519 key; the exact bytes
		// don't matter, only that peer.Decode can parse it.
		Multiaddr: "/ip4/127.0.0.1/tcp/9900/p2p/12D3KooWBaKBVRUuCU4xWHYWZkb76JwZNHspfFUH5MLb7EooFZhf",
	}
}

func TestEncodeContactCardNoPrefix(t *testing.T) {
	card := fixtureCard()
	hash, err := EncodeContactCard(card)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Current encoder emits no prefix.
	if strings.HasPrefix(hash, CardHashPrefix) || strings.HasPrefix(hash, CardHashPrefix2) {
		t.Errorf("encoded hash should not carry a legacy prefix: %q", hash)
	}
	// Round-trip.
	back, err := DecodeContactCard(hash)
	if err != nil {
		t.Fatalf("decode bare: %v", err)
	}
	if back.PublicKey != card.PublicKey {
		t.Errorf("public key mismatch: %q vs %q", back.PublicKey, card.PublicKey)
	}
	if back.Multiaddr != card.Multiaddr {
		t.Errorf("multiaddr mismatch: %q vs %q", back.Multiaddr, card.Multiaddr)
	}
}

func TestDecodeLegacyPrefixedHash(t *testing.T) {
	card := fixtureCard()
	bare, err := EncodeContactCard(card)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Old shared strings have an SLF2. prefix. The decoder should
	// accept them transparently.
	prefixed := CardHashPrefix2 + bare
	back, err := DecodeContactCard(prefixed)
	if err != nil {
		t.Fatalf("decode prefixed: %v", err)
	}
	if back.Multiaddr != card.Multiaddr {
		t.Errorf("multiaddr mismatch after prefix strip")
	}
}

func TestParseAnyContactCardJSONPath(t *testing.T) {
	card := fixtureCard()
	js := `{"version":2,"public_key":"` + card.PublicKey + `","multiaddr":"` + card.Multiaddr + `","name":"Test"}`
	back, err := ParseAnyContactCard(js)
	if err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if back.Name != "Test" {
		t.Errorf("name mismatch: %q", back.Name)
	}
}

func TestParseAnyContactCardHashPath(t *testing.T) {
	card := fixtureCard()
	hash, err := EncodeContactCard(card)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := ParseAnyContactCard(hash)
	if err != nil {
		t.Fatalf("parse hash: %v", err)
	}
	if back.PublicKey != card.PublicKey {
		t.Errorf("public key round-trip mismatch")
	}
}

func TestParseAnyContactCardGarbage(t *testing.T) {
	if _, err := ParseAnyContactCard(""); err == nil {
		t.Error("empty input should error")
	}
	if _, err := ParseAnyContactCard("not a hash or json"); err == nil {
		t.Error("garbage input should error")
	}
}
