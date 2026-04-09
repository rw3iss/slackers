# Testing the P2P stack locally

The friends / secure-mode / libp2p layer is hard to exercise in a unit test because it needs two processes that can reach each other over the network. This is the canonical loop for verifying it by hand.

## Two instances on one machine

1. Enable Secure Mode in your primary instance (Settings → Behavior → Secure Mode). Note its P2P port (default `9900`).
2. Launch a second instance with its own config directory and a different port:
   ```bash
   XDG_CONFIG_HOME=/tmp/slackers-test slackers
   ```
3. In the second instance, set **Settings → Behavior → Secure Mode = on** and **P2P Port = 9901**, then restart it.
4. Optionally give it a distinct display name in **Settings → Friends Config → Edit My Info**.

You now have two independent slackers processes with separate friend stores, keypairs, histories, and P2P nodes on `127.0.0.1:9900` and `127.0.0.1:9901`.

## Exchanging contact cards

- In instance B: **Settings → Friends Config → Share My Info**. Copy the JSON or SLF2 hash.
- In instance A: **Settings → Friends Config → Add a Friend**, press `Ctrl+J` to paste, `Ctrl+S` to save. A friend request handshake runs automatically.
- Accept on the other side.
- Click the new friend in the sidebar. Type a message. It should arrive in the other instance's sidebar as an unread friend chat.

## What to verify

- **Key exchange:** both sides compute the same `PairKey` from ECDH + HKDF.
- **Online detection:** the friend ping cycle turns the sidebar entry green within the ping interval (default 5s).
- **Pending messages:** shut down instance B, send messages from A, restart B. Messages should arrive on reconnect in original order (ordered via `tea.Sequence`, timestamped with original send time).
- **Profile auto-sync:** change your email/name in B. Next offline→online transition, A receives a `MsgTypeProfileSync` and merges.
- **P2P file transfer:** attach a file in a friend chat. The receiver gets a `[FILE:name]` row with a `p2p://` URL; clicking it downloads via the `/slackers/file/1.0.0` stream protocol.

## Automated tests

- `internal/secure/secure_test.go` — X25519, HKDF, ChaCha20-Poly1305 round trips.
- `internal/friends/friends_test.go` — FriendStore CRUD, conflict resolution, SLF2 hash round-trips.

Run with `make test`.

## Networking gotchas

- libp2p tries UPnP and hole punching. On most home routers this works without manual port forwarding, but if two peers can't see each other across NAT, manually forward the P2P port.
- Firewalls (especially on Linux distros with strict `firewalld` defaults) block inbound TCP on non-standard ports. `slackers friends` prints platform-specific firewall commands.
- `P2PNode.ConnectToPeer` has a 3-second dial timeout, and `connectFriend` dispatches dials in a goroutine so a stale peer never blocks the Bubbletea update loop. Don't add blocking network calls inside `Update`.
