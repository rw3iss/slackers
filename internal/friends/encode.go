package friends

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
)

// peerIDToBytes parses a base58btc-encoded libp2p peer ID into its
// raw multihash bytes (~38 bytes for an Ed25519 identity).
func peerIDToBytes(s string) ([]byte, error) {
	pid, err := peer.Decode(s)
	if err != nil {
		return nil, err
	}
	return []byte(pid), nil
}

// peerIDFromBytes converts raw multihash bytes back into the
// canonical base58btc string form.
func peerIDFromBytes(b []byte) (string, error) {
	pid, err := peer.IDFromBytes(b)
	if err != nil {
		return "", err
	}
	return pid.String(), nil
}

// CardHashPrefix is the legacy gzip+JSON contact-card encoding
// prefix. As of this version slackers no longer EMITS either this
// or CardHashPrefix2 on encode — it's still stripped on decode so
// old shared strings (e.g. cards copied into someone's clipboard
// months ago, or baked into Slack DM invite messages) continue to
// round-trip. New cards are bare base64url of the current binary
// layout with no prefix at all.
const CardHashPrefix = "SLF1."

// CardHashPrefix2 is the legacy compact binary contact-card
// prefix. See CardHashPrefix above — kept only for decode-time
// backwards compatibility.
//
// The current encoding layout (unchanged from SLF2 apart from the
// dropped prefix) is:
//
//	[1] version (currently 2)
//	[32] X25519 public key, raw
//	[1] peer_id length (P)
//	[P] libp2p peer ID, raw multihash bytes
//	[4] IPv4 address (big-endian)
//	[2] TCP port (big-endian)
//
// Optional fields like display name, email, and slacker_id are
// omitted entirely — they get filled in after the first connection
// (the friend stores a placeholder name derived from the peer ID
// until a real one arrives in-band).
const CardHashPrefix2 = "SLF2."

// compactCardSchemaVersion is the byte stamped at the start of the
// SLF2 payload so future format revisions can be detected on decode.
const compactCardSchemaVersion byte = 2

// EncodeContactCard returns the compact contact-card hash. Only the
// fields needed to establish a connection are included (X25519
// public key, libp2p peer ID, IPv4, port). Display name, email,
// and slacker_id are dropped — the friend's slacker instance fills
// in a placeholder name from the peer ID and replaces it once the
// first message arrives.
//
// The output is bare `base64url(<binary blob>)` — typically ~105
// chars, with no version prefix. Decoders detect the format by
// attempting the base64url + binary layout parse; they also strip
// a legacy SLF1./SLF2. prefix if someone pastes an old shared
// string into Add a Friend or /add-friend.
func EncodeContactCard(card ContactCard) (string, error) {
	pub, err := base64.StdEncoding.DecodeString(card.PublicKey)
	if err != nil {
		return "", fmt.Errorf("contact hash: invalid public_key base64: %w", err)
	}
	if len(pub) != 32 {
		return "", fmt.Errorf("contact hash: public_key must be 32 bytes, got %d", len(pub))
	}

	peerBytes, ip4, port, err := splitMultiaddr(card.Multiaddr)
	if err != nil {
		return "", fmt.Errorf("contact hash: %w", err)
	}
	if len(peerBytes) > 255 {
		return "", fmt.Errorf("contact hash: peer id too long (%d bytes)", len(peerBytes))
	}

	var buf bytes.Buffer
	buf.WriteByte(compactCardSchemaVersion)
	buf.Write(pub)
	buf.WriteByte(byte(len(peerBytes)))
	buf.Write(peerBytes)
	buf.Write(ip4[:])
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	buf.Write(portBytes[:])

	return base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// DecodeContactCard parses any supported hash format back into a
// ContactCard. The primary path is the bare compact binary layout
// (base64url → schema-versioned binary). Legacy `SLF2.` and `SLF1.`
// prefixes are stripped transparently so cards shared before the
// prefix was dropped still round-trip cleanly. If the compact
// parse fails, the legacy gzip+JSON form is tried as a second
// fallback.
func DecodeContactCard(s string) (ContactCard, error) {
	s = strings.TrimSpace(s)
	// Strip any legacy prefixes. Current encoder emits neither,
	// but accepting them here keeps older shared strings valid.
	trimmed := s
	if p := strings.TrimPrefix(trimmed, CardHashPrefix2); p != trimmed {
		return decodeCompactCard(p)
	}
	if p := strings.TrimPrefix(trimmed, CardHashPrefix); p != trimmed {
		// SLF1 was always the legacy gzip+JSON form.
		if card, err := decodeLegacyCard(p); err == nil {
			return card, nil
		}
	}
	// No recognised prefix — assume it's the current bare format.
	if card, err := decodeCompactCard(s); err == nil {
		return card, nil
	}
	// Last-ditch fallback: maybe it's a legacy gzip+JSON blob
	// someone stripped the prefix from.
	if card, err := decodeLegacyCard(s); err == nil {
		return card, nil
	}
	return ContactCard{}, fmt.Errorf("not a valid contact card hash")
}

// decodeCompactCard parses the SLF2 binary payload (without prefix)
// into a ContactCard. Missing fields (Name/Email/SlackerID) are left
// blank — the import path generates a placeholder name from the
// peer ID.
func decodeCompactCard(payload string) (ContactCard, error) {
	var card ContactCard
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return card, fmt.Errorf("contact hash: invalid base64: %w", err)
	}
	if len(raw) < 1+32+1+1+4+2 {
		return card, fmt.Errorf("contact hash: payload too short (%d bytes)", len(raw))
	}
	off := 0
	if raw[off] != compactCardSchemaVersion {
		return card, fmt.Errorf("contact hash: unsupported version %d", raw[off])
	}
	off++
	pub := raw[off : off+32]
	off += 32
	peerLen := int(raw[off])
	off++
	if off+peerLen+6 > len(raw) {
		return card, fmt.Errorf("contact hash: truncated peer id")
	}
	peerBytes := raw[off : off+peerLen]
	off += peerLen
	ip := net.IPv4(raw[off], raw[off+1], raw[off+2], raw[off+3])
	off += 4
	port := binary.BigEndian.Uint16(raw[off : off+2])

	peerID, err := peerIDFromBytes(peerBytes)
	if err != nil {
		return card, fmt.Errorf("contact hash: invalid peer id: %w", err)
	}

	card.Version = 2
	card.PublicKey = base64.StdEncoding.EncodeToString(pub)
	card.Multiaddr = fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip.String(), port, peerID)
	card.SlackerID = peerID // peer ID doubles as the unique slacker id
	// Name and Email are intentionally left blank — SLF2 doesn't
	// carry them. Callers that need a display string should fall
	// back to ShortPeerID(card) or use friends.FallbackName(card).
	return card, nil
}

// ShortPeerID returns a short, human-friendly identifier derived
// from a contact card. Prefers the trailing 8 chars of the libp2p
// peer ID embedded in the multiaddr (e.g. "WFKy7Pmh"), falling
// back to the SlackerID. Used for placeholder display labels when
// the card has no Name/Email.
func ShortPeerID(card ContactCard) string {
	if card.Multiaddr != "" {
		parts := strings.Split(strings.TrimPrefix(card.Multiaddr, "/"), "/")
		if len(parts) >= 6 && parts[4] == "p2p" {
			return shortID(parts[5])
		}
	}
	if card.SlackerID != "" {
		return shortID(card.SlackerID)
	}
	return ""
}

// FallbackName returns a synthetic display name for a contact card
// that lacks an explicit Name. Uses Email when present, otherwise
// "Friend <shortPeerID>".
func FallbackName(card ContactCard) string {
	if s := strings.TrimSpace(card.Email); s != "" {
		return s
	}
	if s := ShortPeerID(card); s != "" {
		return "Friend " + s
	}
	return "Friend"
}

// decodeLegacyCard handles the SLF1 gzip+JSON form for backwards
// compatibility with older shared hashes.
func decodeLegacyCard(payload string) (ContactCard, error) {
	var card ContactCard
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return card, fmt.Errorf("contact hash: invalid base64: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return card, fmt.Errorf("contact hash: invalid gzip: %w", err)
	}
	defer zr.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, zr); err != nil {
		return card, fmt.Errorf("contact hash: decompress: %w", err)
	}
	if err := json.Unmarshal(out.Bytes(), &card); err != nil {
		return card, fmt.Errorf("contact hash: invalid json payload: %w", err)
	}
	return card, nil
}

// splitMultiaddr extracts (peer-id-bytes, ipv4, port) from a libp2p
// multiaddr string of the form /ip4/<ip>/tcp/<port>/p2p/<peerID>.
// Only IPv4+TCP+p2p is supported on the encode side; other forms
// are rejected to keep the format compact.
func splitMultiaddr(maddr string) ([]byte, [4]byte, int, error) {
	var ip4 [4]byte
	parts := strings.Split(strings.TrimPrefix(maddr, "/"), "/")
	if len(parts) < 6 || parts[0] != "ip4" || parts[2] != "tcp" || parts[4] != "p2p" {
		return nil, ip4, 0, fmt.Errorf("multiaddr must be /ip4/<ip>/tcp/<port>/p2p/<peerID>, got %q", maddr)
	}
	ip := net.ParseIP(parts[1]).To4()
	if ip == nil {
		return nil, ip4, 0, fmt.Errorf("invalid ipv4 in multiaddr: %s", parts[1])
	}
	copy(ip4[:], ip)
	port, err := strconv.Atoi(parts[3])
	if err != nil || port <= 0 || port > 65535 {
		return nil, ip4, 0, fmt.Errorf("invalid port in multiaddr: %s", parts[3])
	}
	peerStr := parts[5]
	peerBytes, err := peerIDToBytes(peerStr)
	if err != nil {
		return nil, ip4, 0, fmt.Errorf("invalid peer id in multiaddr: %w", err)
	}
	return peerBytes, ip4, port, nil
}

// shortID returns the trailing 8 chars of a libp2p peer ID, used as
// a friendly placeholder name when the contact card omits the real
// display name (e.g. "Friend WFKy7Pmh").
func shortID(peerID string) string {
	if len(peerID) <= 8 {
		return peerID
	}
	return peerID[len(peerID)-8:]
}

// ParseAnyContactCard accepts either a raw JSON contact card or a
// hash-encoded one and returns the resulting ContactCard. Used by
// the Add Friend paste handler and the CLI import-friend command so
// a single function can route both input formats.
//
// Detection order:
//  1. Leading "{" → JSON unmarshal.
//  2. Anything else → DecodeContactCard (which handles bare hash,
//     legacy SLF2./SLF1. prefixes, and the old gzip+JSON fallback).
func ParseAnyContactCard(input string) (ContactCard, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ContactCard{}, fmt.Errorf("empty contact card input")
	}
	if strings.HasPrefix(trimmed, "{") {
		var card ContactCard
		if err := json.Unmarshal([]byte(trimmed), &card); err != nil {
			return card, fmt.Errorf("invalid contact JSON: %w", err)
		}
		return card, nil
	}
	return DecodeContactCard(trimmed)
}
