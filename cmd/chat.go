package cmd

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nigel-dev/pgp-chat/internal/chat"
	"github.com/nigel-dev/pgp-chat/internal/p2p"
	"github.com/nigel-dev/pgp-chat/internal/pgp"
	"github.com/nigel-dev/pgp-chat/internal/ui/client"
	"github.com/spf13/cobra"
)

type chatFlags struct {
	listenAddr string
	nodeKey    string
	privateKey string
	peerKey    string
	passphrase string
	relays     []string
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
		identity, err := pgp.LoadIdentity(listenFlags.privateKey, listenFlags.peerKey, listenFlags.passphrase)
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
		return runTUI(chat.NewSession(conn, identity), debugEnabled)
	},
}

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to a libp2p chat peer",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if connectPeerAddr == "" {
			return fmt.Errorf("--peer is required")
		}
		identity, err := pgp.LoadIdentity(connectFlags.privateKey, connectFlags.peerKey, connectFlags.passphrase)
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
		return runTUI(chat.NewSession(conn, identity), debugEnabled)
	},
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
	cmd.Flags().StringVar(&flags.passphrase, "passphrase-file", flags.passphrase, "file containing the local key passphrase")
	cmd.Flags().StringSliceVar(&flags.relays, "relay", flags.relays, "static libp2p relay multiaddress; repeat as needed")
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
