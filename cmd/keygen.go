package cmd

import (
	"fmt"
	"os"

	"github.com/nigel-dev/pgp-chat/internal/pgp"
	"github.com/spf13/cobra"
)

var (
	keygenName        string
	keygenEmail       string
	keygenPrivatePath string
	keygenPublicPath  string
	keygenPassphrase  string
	keygenForce       bool
)

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate an OpenPGP identity key pair",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if keygenName == "" || keygenEmail == "" {
			return fmt.Errorf("both --name and --email are required")
		}
		if !keygenForce {
			for _, path := range []string{keygenPrivatePath, keygenPublicPath} {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("refusing to overwrite %q; use --force", path)
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("check key path %q: %w", path, err)
				}
			}
		}

		key, err := pgp.GenerateKey(keygenName, keygenEmail)
		if err != nil {
			return err
		}
		var passphrase []byte
		if keygenPassphrase != "" {
			passphrase, err = os.ReadFile(keygenPassphrase)
			if err != nil {
				return fmt.Errorf("read passphrase file: %w", err)
			}
			for len(passphrase) > 0 && (passphrase[len(passphrase)-1] == '\n' || passphrase[len(passphrase)-1] == '\r') {
				passphrase = passphrase[:len(passphrase)-1]
			}
		}
		if err := pgp.SaveKeyPair(key, keygenPrivatePath, keygenPublicPath, passphrase); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "private key: %s\npublic key:  %s\nfingerprint: %s\n", keygenPrivatePath, keygenPublicPath, pgp.Fingerprint(key))
		if len(passphrase) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "warning: private key is not passphrase-protected")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(keygenCmd)
	keygenCmd.Flags().StringVar(&keygenName, "name", "", "identity name")
	keygenCmd.Flags().StringVar(&keygenEmail, "email", "", "identity email")
	keygenCmd.Flags().StringVar(&keygenPrivatePath, "private-key", "private.asc", "private key output path")
	keygenCmd.Flags().StringVar(&keygenPublicPath, "public-key", "public.asc", "public key output path")
	keygenCmd.Flags().StringVar(&keygenPassphrase, "passphrase-file", "", "file containing the private-key passphrase")
	keygenCmd.Flags().BoolVar(&keygenForce, "force", false, "overwrite existing key files")
}
