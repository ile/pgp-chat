package cmd

import (
	"fmt"
	"os"

	"github.com/nigel-dev/pgp-chat/internal/pgp"
	chatgui "github.com/nigel-dev/pgp-chat/internal/ui/gui"
	"github.com/spf13/cobra"
)

var (
	keygenName        string
	keygenEmail       string
	keygenPrivatePath string
	keygenPublicPath  string
	keygenPassphrase  string
	keygenForce       bool
	keygenGUI         bool
)

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate an OpenPGP identity key pair",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if keygenGUI {
			return runGUIKeygen(cmd)
		}
		if keygenName == "" {
			return fmt.Errorf("--name is required")
		}
		var passphrase []byte
		var err error
		if keygenPassphrase != "" {
			passphrase, err = os.ReadFile(keygenPassphrase)
			if err != nil {
				return fmt.Errorf("read passphrase file: %w", err)
			}
			defer clearBytes(passphrase)
			for len(passphrase) > 0 && (passphrase[len(passphrase)-1] == '\n' || passphrase[len(passphrase)-1] == '\r') {
				passphrase = passphrase[:len(passphrase)-1]
			}
		}
		fingerprint, err := generateAndSaveKey(keygenName, keygenEmail, keygenPrivatePath, keygenPublicPath, passphrase, keygenForce)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "private key: %s\npublic key:  %s\nfingerprint: %s\n", keygenPrivatePath, keygenPublicPath, fingerprint)
		if len(passphrase) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "warning: private key is not passphrase-protected")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(keygenCmd)
	keygenCmd.Flags().StringVar(&keygenName, "name", "", "identity name")
	keygenCmd.Flags().StringVar(&keygenEmail, "email", "", "optional identity email")
	keygenCmd.Flags().StringVar(&keygenPrivatePath, "private-key", "private.asc", "private key output path")
	keygenCmd.Flags().StringVar(&keygenPublicPath, "public-key", "public.asc", "public key output path")
	keygenCmd.Flags().StringVar(&keygenPassphrase, "passphrase-file", "", "file containing the private-key passphrase")
	keygenCmd.Flags().BoolVar(&keygenForce, "force", false, "overwrite existing key files")
	keygenCmd.Flags().BoolVar(&keygenGUI, "gui", false, "use a graphical form instead of command-line flags")
}

func runGUIKeygen(cmd *cobra.Command) error {
	if keygenPassphrase != "" {
		return fmt.Errorf("--passphrase-file cannot be combined with --gui; enter the passphrase in the GUI")
	}

	privatePath := keygenPrivatePath
	publicPath := keygenPublicPath
	output, err := chatgui.RunKeygenForm(chatgui.KeygenOptions{
		Name:        keygenName,
		Email:       keygenEmail,
		PrivatePath: keygenPrivatePath,
		PublicPath:  keygenPublicPath,
		Force:       keygenForce,
	}, func(options chatgui.KeygenOptions) (string, error) {
		privatePath = options.PrivatePath
		publicPath = options.PublicPath
		fingerprint, err := generateAndSaveKey(
			options.Name,
			options.Email,
			options.PrivatePath,
			options.PublicPath,
			options.Passphrase,
			options.Force,
		)
		if err != nil {
			return "", err
		}
		output := fmt.Sprintf("private key: %s\npublic key:  %s\nfingerprint: %s", privatePath, publicPath, fingerprint)
		if len(options.Passphrase) == 0 {
			output += "\nwarning: private key is not passphrase-protected"
		}
		return output, nil
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), output)
	return nil
}

func generateAndSaveKey(name, email, privatePath, publicPath string, passphrase []byte, force bool) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if privatePath == "" || publicPath == "" {
		return "", fmt.Errorf("both private and public key paths are required")
	}
	if privatePath == publicPath {
		return "", fmt.Errorf("private and public key paths must be different")
	}
	if !force {
		for _, path := range []string{privatePath, publicPath} {
			if _, err := os.Stat(path); err == nil {
				return "", fmt.Errorf("refusing to overwrite %q; use --force", path)
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("check key path %q: %w", path, err)
			}
		}
	}

	key, err := pgp.GenerateKey(name, email)
	if err != nil {
		return "", err
	}
	if err := pgp.SaveKeyPair(key, privatePath, publicPath, passphrase); err != nil {
		return "", err
	}
	return pgp.Fingerprint(key), nil
}
