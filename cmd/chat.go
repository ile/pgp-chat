package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nigel-dev/pgp-chat/internal/chat"
	"github.com/nigel-dev/pgp-chat/internal/p2p"
	"github.com/nigel-dev/pgp-chat/internal/pgp"
	"github.com/nigel-dev/pgp-chat/internal/ui/client"
	chatgui "github.com/nigel-dev/pgp-chat/internal/ui/gui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type chatFlags struct {
	listenAddr string
	nodeKey    string
	privateKey string
	peerKey    string
	passphrase string
	relays     []string
	gui        bool
}

var listenFlags = chatFlags{
	listenAddr: "/ip4/0.0.0.0/tcp/0",
	nodeKey:    "node.key",
	privateKey: "private.asc",
	peerKey:    "peer-public.asc",
}

var connectFlags = chatFlags{
	listenAddr: "/ip4/0.0.0.0/tcp/0",
	nodeKey:    "node.key",
	privateKey: "private.asc",
	peerKey:    "peer-public.asc",
}

var (
	listenExpectedPeer string
	connectPeerAddr    string
)

var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Listen for one libp2p chat connection",
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity, err := loadIdentity(listenFlags)
		if err != nil {
			return err
		}
		node, err := p2p.NewNode(p2p.Config{
			ListenAddr: listenFlags.listenAddr,
			NodeKey:    listenFlags.nodeKey,
			Relays:     listenFlags.relays,
		})
		if err != nil {
			return err
		}
		defer node.Close()

		printNodeInfo(cmd, node, identity)
		fmt.Fprintln(cmd.OutOrStdout(), "waiting for a peer...")
		conn, err := node.Accept(context.Background(), listenExpectedPeer)
		if err != nil {
			return err
		}
		defer conn.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "connected to libp2p peer %s\n", conn.RemotePeer())
		return runSession(chat.NewSession(conn, identity), listenFlags.gui)
	},
}

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to a libp2p chat peer",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if connectPeerAddr == "" {
			return fmt.Errorf("--peer is required")
		}
		identity, err := loadIdentity(connectFlags)
		if err != nil {
			return err
		}
		node, err := p2p.NewNode(p2p.Config{
			ListenAddr: connectFlags.listenAddr,
			NodeKey:    connectFlags.nodeKey,
			Relays:     connectFlags.relays,
		})
		if err != nil {
			return err
		}
		defer node.Close()

		printNodeInfo(cmd, node, identity)
		conn, remotePeer, err := node.Dial(context.Background(), connectPeerAddr)
		if err != nil {
			return err
		}
		defer conn.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "connected to libp2p peer %s\n", remotePeer)
		return runSession(chat.NewSession(conn, identity), connectFlags.gui)
	},
}

func loadIdentity(flags chatFlags) (*pgp.Identity, error) {
	if flags.passphrase != "" {
		return pgp.LoadIdentity(flags.privateKey, flags.peerKey, flags.passphrase)
	}

	identity, err := pgp.LoadIdentity(flags.privateKey, flags.peerKey, "")
	if !errors.Is(err, pgp.ErrPassphraseRequired) {
		return identity, err
	}

	validate := func(passphrase []byte) error {
		var validateErr error
		identity, validateErr = pgp.LoadIdentityWithPassphrase(flags.privateKey, flags.peerKey, passphrase)
		return validateErr
	}
	if flags.gui {
		err = chatgui.PromptPassphrase(validate)
	} else {
		err = promptPassphrase(validate)
	}
	if err != nil {
		return nil, err
	}
	return identity, nil
}

func promptPassphrase(validate func([]byte) error) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("private key is locked; use --passphrase-file when stdin is not a terminal")
	}

	for {
		fmt.Fprint(os.Stderr, "Private key passphrase: ")
		passphrase, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read passphrase: %w", err)
		}
		if err := validate(passphrase); err != nil {
			clearBytes(passphrase)
			fmt.Fprintf(os.Stderr, "Unlock failed: %v\nTry again or press Ctrl-C to cancel.\n", err)
			continue
		}
		clearBytes(passphrase)
		return nil
	}
}

func clearBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

func init() {
	rootCmd.AddCommand(listenCmd, connectCmd)
	addChatFlags(listenCmd, &listenFlags)
	addChatFlags(connectCmd, &connectFlags)
	listenCmd.Flags().StringVar(&listenExpectedPeer, "expected-peer", "", "only accept this libp2p PeerID")
	connectCmd.Flags().StringVar(&connectPeerAddr, "peer", "", "peer multiaddress including /p2p/<PeerID>")
}

func addChatFlags(cmd *cobra.Command, flags *chatFlags) {
	cmd.Flags().StringVar(&flags.listenAddr, "listen", flags.listenAddr, "libp2p listen multiaddress")
	cmd.Flags().StringVar(&flags.nodeKey, "node-key", flags.nodeKey, "persistent libp2p identity key")
	cmd.Flags().StringVar(&flags.privateKey, "private-key", flags.privateKey, "local OpenPGP private key")
	cmd.Flags().StringVar(&flags.peerKey, "peer-key", flags.peerKey, "peer OpenPGP public key")
	cmd.Flags().StringVar(&flags.passphrase, "passphrase-file", flags.passphrase, "file containing the local key passphrase (optional; prompts if omitted)")
	cmd.Flags().StringSliceVar(&flags.relays, "relay", flags.relays, "static libp2p relay multiaddress; repeat as needed")
	cmd.Flags().BoolVar(&flags.gui, "gui", false, "use the Fyne GUI instead of the terminal UI")
}

func printNodeInfo(cmd *cobra.Command, node *p2p.Node, identity *pgp.Identity) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "libp2p PeerID: %s\n", node.ID())
	fmt.Fprintf(out, "OpenPGP fingerprint: %s\n", pgp.Fingerprint(identity.Public))
	addresses := node.Addresses()
	if len(addresses) == 0 {
		fmt.Fprintln(out, "libp2p addresses: none")
		return
	}
	fmt.Fprintln(out, "libp2p addresses:")
	for _, address := range addresses {
		fmt.Fprintf(out, "  %s\n", address)
	}
}

func runTUI(session *chat.Session, debug bool) error {
	model, logger := client.New(debug, session)
	if logger != nil {
		defer logger.Close()
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

func runSession(session *chat.Session, useGUI bool) error {
	if useGUI {
		return chatgui.Run(session)
	}
	return runTUI(session, debugEnabled)
}
