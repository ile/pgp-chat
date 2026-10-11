package cmd

import (
	"errors"
	"fmt"
	"net"

	"github.com/nigel-dev/pgp-chat/internal/chat"
	"github.com/nigel-dev/pgp-chat/internal/pgp"
	"github.com/nigel-dev/pgp-chat/internal/proxy"
	"github.com/spf13/cobra"
)

var proxyListen = "127.0.0.1:4001"
var joinServer string
var joinRoom string
var joinFlags = chatFlags{privateKey: "private.asc", peerKey: "peer-public.asc"}

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Run a two-person ciphertext forwarding server",
	RunE: func(cmd *cobra.Command, _ []string) error {
		listener, err := net.Listen("tcp", proxyListen)
		if err != nil {
			return fmt.Errorf("listen for proxy clients: %w", err)
		}
		defer listener.Close()
		fmt.Fprintf(cmd.OutOrStdout(), "proxy listening on %s\n", listener.Addr())
		return proxy.Serve(cmd.Context(), listener)
	},
}

var joinCmd = &cobra.Command{
	Use:   "join",
	Short: "Join a private chat room through a proxy",
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity, err := loadIdentity(joinFlags)
		interactiveGUI := joinFlags.gui && errors.Is(err, pgp.ErrPassphraseRequired)
		if err != nil && !interactiveGUI {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "waiting for another user in the room...")
		conn, err := proxy.Join(cmd.Context(), joinServer, joinRoom)
		if err != nil {
			return err
		}
		defer conn.Close()
		fmt.Fprintln(cmd.OutOrStdout(), "connected through proxy")
		if interactiveGUI {
			return runGUIWithPassphrase(conn, joinFlags)
		}
		return runSession(chat.NewSession(conn, identity), joinFlags.gui)
	},
}

func init() {
	rootCmd.AddCommand(proxyCmd, joinCmd)
	proxyCmd.Flags().StringVar(&proxyListen, "listen", proxyListen, "local TCP bind address on the proxy server (use 0.0.0.0:PORT for remote clients)")
	joinCmd.Flags().StringVar(&joinServer, "server", "", "reachable proxy server host:port (do not use 0.0.0.0)")
	joinCmd.Flags().StringVar(&joinRoom, "room", "", "shared room code (8–64 letters, digits, underscores or hyphens)")
	joinCmd.Flags().StringVar(&joinFlags.privateKey, "private-key", joinFlags.privateKey, "local OpenPGP private key")
	joinCmd.Flags().StringVar(&joinFlags.peerKey, "peer-key", joinFlags.peerKey, "peer OpenPGP public key")
	joinCmd.Flags().StringVar(&joinFlags.passphrase, "passphrase-file", "", "file containing the local key passphrase (optional; prompts if omitted)")
	joinCmd.Flags().BoolVar(&joinFlags.gui, "gui", false, "use the Fyne GUI instead of the terminal UI")
}
