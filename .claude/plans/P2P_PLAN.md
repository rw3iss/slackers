# Slackers P2P Secure Messaging - Implementation Plan

## Overview

End-to-end encrypted, peer-to-peer messaging between Slackers users. Messages bypass Slack's servers entirely when a direct connection is possible, with encrypted Slack DM relay as a fallback. Slack cannot read the messages in either mode.

## Architecture

```
User A (Slackers)                    User B (Slackers)
┌──────────────────┐                ┌──────────────────┐
│ 1. Generate      │                │ 1. Generate      │
│    X25519 keypair│                │    X25519 keypair │
│ 2. Publish pubkey│───Slack API───►│ 2. Publish pubkey│
│    to profile    │                │    to profile    │
│ 3. Detect peer   │◄──Slack API───│ 3. Detect peer   │
│ 4. Key exchange  │◄──Encrypted──►│ 4. Key exchange  │
│    via Slack DM  │   Slack DM    │    via Slack DM  │
│ 5. P2P connect   │◄════libp2p═══►│ 5. P2P connect   │
│    (direct/relay)│               │    (direct/relay)│
│ 6. Send/receive  │◄══encrypted══►│ 6. Send/receive  │
│    messages      │               │    messages      │
└──────────────────┘                └──────────────────┘

Fallback: If P2P fails, messages are E2E encrypted and sent
through Slack DMs. Slack sees ciphertext only.
```

## User Experience

1. Enable "Secure Mode" in settings
2. App generates a keypair and publishes fingerprint to Slack profile
3. Open a DM with another Slackers user who has Secure Mode enabled
4. Status bar shows connection state:
   - `[P2P]` -- direct peer-to-peer connection active
   - `[E2E]` -- encrypted via Slack relay (P2P unavailable)
   - (nothing) -- normal unencrypted Slack mode
5. Messages look identical in both modes
6. Whitelist specific users for automatic secure connections

## Config & Settings

New settings in `Ctrl-S`:

| Setting | Default | Description |
|---------|---------|-------------|
| Secure Mode | off | Enable E2E encrypted P2P messaging |
| P2P Port | 9900 | Local port for P2P listener |
| P2P Address | (auto) | Public IP/domain override (auto-detected if blank) |
| Secure Whitelist | [] | User IDs/names to always connect securely with |

New config fields in `config.json`:

```json
{
  "secure_mode": false,
  "p2p_port": 9900,
  "p2p_address": "",
  "secure_whitelist": [],
  "secure_private_key": "~/.config/slackers/secure.key"
}
```

## Phases

### Phase 1: Key Management & Peer Discovery

**Goal**: Generate keypairs, publish to Slack, detect other Slackers users.

**Files**:
- `internal/secure/keys.go` -- X25519 keypair generation, storage, loading
- `internal/secure/discovery.go` -- Slack profile field management, peer detection
- `internal/config/config.go` -- new secure mode fields

**Key generation**:
- Use `crypto/rand` + `golang.org/x/crypto/curve25519` for X25519
- Generate on first enable of Secure Mode
- Store private key at `~/.config/slackers/secure.key` (0600 permissions)
- Public key stored in config as base64

**Discovery**:
- Use `users.profile.set` to set custom field `slackers_p2p` = base64 public key
- When opening a DM, call `users.profile.get` on the other user
- If `slackers_p2p` field exists and is valid, peer is Slackers P2P capable
- Cache discovered peers in memory to avoid repeated API calls

**Slack service additions**:
- `SetProfileField(key, value string) error`
- `GetProfileField(userID, key string) (string, error)`

### Phase 2: E2E Encrypted Slack DM Relay

**Goal**: Encrypt messages sent through Slack DMs. Works everywhere, no P2P needed.

**Files**:
- `internal/secure/crypto.go` -- encryption/decryption using shared secret
- `internal/secure/session.go` -- session management per peer
- `internal/tui/model.go` -- intercept send/receive for secure channels

**Crypto**:
- X25519 key exchange: derive shared secret from own private key + peer's public key
- Use shared secret with HKDF to derive encryption key
- ChaCha20-Poly1305 AEAD encryption for each message
- Each message includes a nonce (incrementing counter)
- Format: `[SLACKERS_E2E:<base64 nonce>:<base64 ciphertext>]`

**Session management**:
- `SecureSession` struct per peer: public key, shared secret, send/recv counters
- Created when a DM is opened with a discovered Slackers peer
- Sessions cached in memory, re-derived from keys on restart

**Message flow (send)**:
1. User types message and hits send
2. If channel is a DM and peer is in whitelist with Secure Mode:
   a. Look up/create SecureSession
   b. Encrypt message with ChaCha20-Poly1305
   c. Send via Slack API as `[SLACKERS_E2E:<nonce>:<ciphertext>]`
3. Otherwise: send normally

**Message flow (receive)**:
1. Message arrives (via poll or socket)
2. If message text matches `[SLACKERS_E2E:...]` pattern:
   a. Parse nonce and ciphertext
   b. Look up SecureSession for sender
   c. Decrypt and display plaintext
3. Otherwise: display normally

**Status indicator**:
- When viewing a secure DM, channel name header shows `[E2E]` badge
- Messages show a small lock icon or indicator

### Phase 3: Direct P2P Connection via libp2p

**Goal**: Establish direct connections between peers for message delivery.

**Dependencies**:
- `github.com/libp2p/go-libp2p` -- core P2P networking
- `github.com/libp2p/go-libp2p/p2p/net/connmgr` -- connection management
- Already handles: NAT traversal, hole punching, relay, multiplexing

**Files**:
- `internal/secure/p2p.go` -- libp2p host setup, listener, message protocol
- `internal/secure/transport.go` -- message framing over P2P streams

**libp2p host**:
- Create a libp2p host on the configured port (default 9900)
- Generate a libp2p identity from the X25519 private key (or separate Ed25519 key)
- Enable AutoNAT for NAT detection
- Enable hole punching for NAT traversal
- Enable circuit relay as fallback (uses public relay nodes)

**Peer connection flow**:
1. User A opens secure DM with User B
2. Exchange libp2p multiaddresses via encrypted Slack DM:
   - A sends: `[SLACKERS_P2P_ADDR:<encrypted multiaddr>]`
   - B responds with their multiaddr
3. Both attempt direct connection via libp2p
4. If direct fails, libp2p automatically falls back to relay
5. Once connected, open a bidirectional stream for messages

**Message protocol**:
- Custom libp2p protocol: `/slackers/msg/1.0.0`
- Messages are length-prefixed protobuf or JSON:
  ```json
  {"type": "message", "text": "hello", "ts": 1712345678}
  ```
- Already encrypted by libp2p's Noise transport layer

**Connection state machine**:
```
DISCONNECTED -> DISCOVERING -> CONNECTING -> CONNECTED
     ^                                          |
     |              (timeout/error)             |
     └──────────────────────────────────────────┘
```

**Fallback strategy**:
1. Try direct libp2p connection (TCP, QUIC)
2. Try libp2p with hole punching
3. Try libp2p with relay
4. Fall back to E2E encrypted Slack DMs (Phase 2)

### Phase 4: UI Integration

**Goal**: Seamless user experience for secure messaging.

**Status indicators**:
- Channel header: `#username [P2P]` or `#username [E2E]`
- Status bar: shows secure connection state when viewing a secure channel
- Message rendering: subtle indicator on encrypted messages (e.g., lock emoji prefix)

**Settings panel additions**:
- Secure Mode: on/off toggle
- P2P Port: editable number
- P2P Address: editable string (blank = auto-detect)
- Manage Whitelist: overlay to add/remove users

**Whitelist management overlay**:
- Lists all DM contacts
- Toggle checkmark to whitelist/unwhitelist
- Shows connection state for each whitelisted user
- Enter to toggle, Esc to close

**Key verification**:
- Show fingerprint in settings: `AB:CD:EF:12:34:56:78:90`
- Users can compare fingerprints out-of-band to verify no MITM

### Phase 5: Polish & Security Hardening

**Forward secrecy**:
- Implement Double Ratchet (Signal protocol) for forward secrecy
- Or use libp2p's built-in Noise protocol which provides it

**Key rotation**:
- Rotate keypairs periodically (configurable)
- Old keys kept briefly for decrypting in-flight messages

**Message persistence**:
- Encrypted messages stored locally for offline viewing
- Decrypted on display using local key

**Offline messages**:
- If peer is offline, queue messages
- Send via encrypted Slack DM relay
- Deliver via P2P when peer comes online

## File Structure

```
internal/
  secure/
    keys.go           Key generation, storage, X25519 operations
    crypto.go         Message encryption/decryption (ChaCha20-Poly1305)
    discovery.go      Slack profile field management, peer detection
    session.go        Per-peer session management
    p2p.go            libp2p host, connection, NAT traversal
    transport.go      Message framing over P2P streams
    whitelist.go      Whitelist management
  tui/
    secure_status.go  Secure mode UI indicators
    whitelist.go      Whitelist management overlay
```

## Dependencies to Add

```
golang.org/x/crypto          -- X25519, ChaCha20-Poly1305, HKDF
github.com/libp2p/go-libp2p  -- P2P networking, NAT traversal, relay
```

## README Section to Add

```markdown
## Secure Messaging (P2P)

Slackers supports end-to-end encrypted peer-to-peer messaging between users
who both have Secure Mode enabled. Messages bypass Slack's servers entirely
when possible.

### How it works

1. Enable Secure Mode in settings (`Ctrl-S`)
2. Your public key is published to your Slack profile
3. When you open a DM with another Slackers user who has Secure Mode enabled,
   a direct P2P connection is established automatically
4. Messages are encrypted with ChaCha20-Poly1305 and sent directly between
   your machines
5. If direct connection fails, messages are still E2E encrypted but relayed
   through Slack (Slack sees only ciphertext)

### Port forwarding

For direct P2P connections, your configured port (default 9900) should be
accessible. If you're behind a NAT:

- **UPnP routers**: libp2p will attempt automatic port mapping
- **Manual**: Forward TCP/UDP port 9900 to your machine
- **Corporate networks**: The app falls back to encrypted Slack relay automatically

Configure your port and address in settings:
- **P2P Port**: The local port to listen on (default 9900)
- **P2P Address**: Your public IP or domain (auto-detected if blank)

### Security

- **X25519** key exchange (Curve25519)
- **ChaCha20-Poly1305** authenticated encryption
- **libp2p Noise** transport security
- **NAT traversal** via hole punching and relay fallback
- Private keys stored locally with 0600 permissions
- Verify peer identity by comparing key fingerprints
```

## Implementation Order

1. Phase 1: Keys + discovery (~2-3 hours)
2. Phase 2: E2E encrypted Slack relay (~3-4 hours)
3. Phase 3: libp2p P2P connection (~4-5 hours)
4. Phase 4: UI integration (~2-3 hours)
5. Phase 5: Polish (~2-3 hours)

Total: ~15-20 hours of implementation

## Security Considerations

- Private keys MUST be stored with 0600 permissions
- Never transmit private keys over the network
- Public keys published to Slack profile are harmless — they're public by design
- The `[SLACKERS_E2E:...]` format in Slack messages is opaque to Slack admins
- Users should verify key fingerprints out-of-band for high-security use
- Consider adding a "verify" command that shows both users' fingerprints
