# PGP Chat

Terminal chat between two peers. The transport is libp2p and every chat
message is signed with the sender's OpenPGP private key and encrypted with the
recipient's OpenPGP public key. The libp2p connection explicitly uses the
Noise secure transport, so the PGP payload is additionally protected by a
forward-secret session channel.

This is an MVP: peers exchange the libp2p multiaddress and OpenPGP public key
out of band. There is no message history, contact database, DHT discovery, or
store-and-forward service yet.

## Build

Requires Go 1.23 or newer.

```bash
go build -o pgp-chat .
```

### Ubuntu/Debian

Install Go 1.23 or newer. The distribution's `golang` package may be too old;
if so, use the official Go download from <https://go.dev/dl/>. On Linux, Fyne
also requires a C compiler and graphics library development headers:

```bash
sudo apt update
sudo apt install gcc pkg-config libgl1-mesa-dev xorg-dev \
  libwayland-dev libxkbcommon-dev
go version
go build -o pgp-chat .
```

### macOS

First install the Xcode Command Line Tools and Go. The full Xcode application
is usually not needed just to build the project:

```bash
xcode-select --install
```

You can install Go from <https://go.dev/dl/> or with Homebrew:

```bash
brew install go
go version
go build -o pgp-chat .
```

The graphical version is selected with `--gui` on either chat command:

```bash
./pgp-chat listen --gui --listen /ip4/0.0.0.0/tcp/4001 ...
./pgp-chat connect --gui --peer /ip4/PEER_IP/tcp/4001/p2p/PEER_ID ...
```

The GUI is built with Fyne and uses a dark, split-pane layout: conversation on
the left, security/peer details on the right, and the message composer at the
bottom. Linux development requires the Fyne graphics development packages
(Wayland/X11/OpenGL headers and a C compiler); macOS development requires the
Xcode command-line tools. See the [Fyne quick start](https://docs.fyne.io/started/quick/)
for platform-specific setup.

## Create keys

Create one key pair per user. The name is required, but the email is optional.
A passphrase file is optional, but recommended:

```bash
./pgp-chat keygen --name Alice --email alice@example.com \
  --private-key alice-private.asc --public-key alice-public.asc \
  --passphrase-file alice.pass

./pgp-chat keygen --name Bob --email bob@example.com \
  --private-key bob-private.asc --public-key bob-public.asc \
  --passphrase-file bob.pass
```

For a graphical key-generation form, use:

```bash
./pgp-chat keygen --gui
```

The form lets you enter the identity details, output paths, and an optional
passphrase with confirmation. Existing files are protected by default; enable
the overwrite checkbox only when replacement is intentional.

Leaving the email empty creates a user ID containing only the chosen name. A
pseudonym can be used if the key should not reveal an email address.

If a private key is protected and `--passphrase-file` is omitted, the TUI asks
for the passphrase in the terminal and the GUI shows a password dialog. The
passphrase is only used to unlock the private key in memory; it is not sent to
the peer. A passphrase file should be readable only by its owner (`chmod 600`)
and must not be committed to the repository.

Exchange the public keys securely. Alice needs Bob's public key as
`alice-peer.asc`, and Bob needs Alice's public key as `bob-peer.asc`.

## Direct connection

Start Alice as the listener. Port-forward the chosen TCP port on Alice's
router if she is behind NAT:

```bash
./pgp-chat listen \
  --listen /ip4/0.0.0.0/tcp/4001 \
  --node-key alice.node.key \
  --private-key alice-private.asc \
  --peer-key alice-peer.asc \
  --passphrase-file alice.pass
```

The command prints an address ending in `/p2p/<PeerID>`. Bob connects using
Alice's reachable public address and that PeerID:

```bash
./pgp-chat connect \
  --peer /ip4/ALICE_PUBLIC_IP/tcp/4001/p2p/ALICE_PEER_ID \
  --node-key bob.node.key \
  --private-key bob-private.asc \
  --peer-key bob-peer.asc \
  --passphrase-file bob.pass
```

After the connection is established, typing a message in either TUI encrypts
and signs it before sending. Received messages are decrypted and signature
checked before they are displayed.

## Relay and NAT traversal

The node enables libp2p relay and hole-punching support. To use a known
libp2p circuit-relay v2 server, pass its full address to both peers:

```bash
--relay /ip4/RELAY_IP/tcp/RELAY_PORT/p2p/RELAY_PEER_ID
```

The relay must already be running as a circuit-relay service. The current MVP
does not include automatic peer discovery or operate a relay server itself;
without a relay, a directly reachable address or manual port forwarding is
needed.

## Commands

- `keygen` creates an OpenPGP key pair.
- `listen` waits for one libp2p peer and starts the TUI.
- `connect` dials a peer multiaddress and starts the TUI.
- `client` starts the old standalone UI demo without a network session.

## Development

```bash
go test ./...
go vet ./...
```

The project is licensed under the MIT License; see [LICENSE](LICENSE).
