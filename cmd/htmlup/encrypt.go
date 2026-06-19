package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/htmlcrypt"
	"github.com/truehhart/htmlup/internal/ui"
)

// newEncryptCmd is an internal, hidden helper: it encrypts a local HTML file to
// a self-decrypting page on disk (no upload), for testing the encryption path.
// Not part of the publish flow and not shown in help.
func newEncryptCmd() *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:    "encrypt <file>",
		Short:  "Encrypt an HTML file to <file>.encrypted.html (internal/testing)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			out := ui.Auto()

			if password == "" {
				password = os.Getenv("HTMLUP_PASSWORD")
			}
			if password == "" {
				return fmt.Errorf("a password is required: pass --password or set HTMLUP_PASSWORD")
			}

			src := args[0]
			plain, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			page, err := htmlcrypt.Encrypt(password, plain)
			if err != nil {
				return err
			}

			dst := strings.TrimSuffix(src, filepath.Ext(src)) + ".encrypted.html"
			if err := os.WriteFile(dst, page, 0o644); err != nil {
				return err
			}
			out.Success("encrypted %s → %s", src, dst)
			return nil
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "encryption password (or set HTMLUP_PASSWORD)")
	return cmd
}
