# P2P / Secure Mode

Slackers uses libp2p for direct peer-to-peer connections between
friends. When Secure Mode is enabled, every friend pair gets an
ECDH-derived shared key and messages are encrypted with
ChaCha20-Poly1305 before they hit the wire.

## Enabling Secure Mode

**Settings → Behavior → Secure Mode** (restart required).

The first time you enable it, slackers generates an X25519 keypair
and stores it at `~/.config/slackers/secure.key`. **Never share
this file.**

## P2P port

The default port is **9900**. Change it in **Settings → Behavior →
P2P Port** if it's already in use, or to run a second test instance
alongside your primary one (see the development workflow doc).

After changing the port, your existing contact card becomes stale —
re-share it via **Friends Config → Share My Info** so peers import
the updated multiaddr.

## NAT, firewalls, and reachability

- libp2p tries UPnP and NAT hole punching automatically.
- Behind a strict NAT, you may need to manually forward the P2P
  port on your router.
- Linux distros with `firewalld` enabled need an explicit allow on
  the port. Run `slackers friends` for platform-specific commands.
- A `connection refused` in the debug log usually means the peer's
  slackers isn't running, or their port has changed since they
  last shared their contact card.

## File transfer

Files attached to friend chat messages flow over a separate libp2p
stream protocol (`/slackers/file/1.0.0`). The sender holds the file
until the receiver requests it; cancel an in-flight upload by
right-clicking the file row.
