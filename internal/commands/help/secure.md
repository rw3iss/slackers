# Encryption

## Algorithms

| Layer | Algorithm |
|---|---|
| Key exchange | X25519 (Curve25519 ECDH) |
| Key derivation | HKDF-SHA256 |
| Message encryption | ChaCha20-Poly1305 (AEAD, 12-byte nonces) |
| Identity | Ed25519 (libp2p peer ID) |

## Local key storage

Your X25519 keypair is at `~/.config/slackers/secure.key` with
`0600` permissions. The file is generated on first launch with
Secure Mode enabled and is never logged or transmitted.

If you lose the file, all per-friend pair keys become invalid —
you'd need to re-handshake with each friend to derive fresh shared
secrets.

## Per-pair keys

When two friends connect for the first time, they exchange
public keys, run ECDH, and derive a per-pair encryption key via
HKDF. The pair key is cached on the friend record so subsequent
sessions don't need a full re-handshake.

A peer rotating their public key invalidates the cached pair key —
slackers detects this on the next profile sync and re-derives.

## Encrypted Slack DMs (relay fallback)

If direct P2P fails, messages can still be exchanged as encrypted
Slack DMs. The wire format is
`[SLACKERS_E2E:<base64-nonce>:<base64-ciphertext>]`. Slack's
servers see only the ciphertext.

## Disk encryption

Friend chat history is stored at
`~/.config/slackers/friend_history/` and is encrypted per-friend
with the same pair key. Importing a backup zip with `--mode merge`
deliberately skips merging encrypted history files since merging
ciphertext by message ID would corrupt them.
