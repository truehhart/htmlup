package provider

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/fsutil"
	"github.com/truehhart/htmlup/internal/htmlcrypt"
	"github.com/truehhart/htmlup/internal/ui"
)

// PasswordPromptValue is the --password value that means "prompt me without
// echo" rather than a literal password — the conventional "-" stand-in for
// interactive input. Recognised by resolvePassword.
const PasswordPromptValue = "-"

// PrepareFiles resolves a publish path to its upload file set, applying HTML
// encryption when a password is supplied. This is the single place pre-upload
// transforms live: providers receive the resulting fs.FS and do nothing but
// upload it — they never see paths, passwords, or the encryption step.
func PrepareFiles(cmd *cobra.Command, path string, out *ui.Output) (fs.FS, error) {
	files, err := fsutil.ResolveFS(path)
	if err != nil {
		return nil, err
	}
	password, err := resolvePassword(cmd, out)
	if err != nil {
		return nil, err
	}
	return htmlcrypt.WrapFS(files, password), nil
}

// resolvePassword determines the HTML-encryption password for a publish, or ""
// to publish unencrypted. Encryption is opt-in: with no --password flag and no
// HTMLUP_PASSWORD set it returns "". --password VALUE uses VALUE; --password -
// prompts without echo (requires a terminal).
func resolvePassword(cmd *cobra.Command, out *ui.Output) (string, error) {
	if FlagChanged(cmd, "password") {
		v, _ := cmd.Flags().GetString("password")
		if v == PasswordPromptValue {
			return out.Prompter(cmd.InOrStdin()).Password("Encryption password", "decrypts in-browser; use 16+ characters")
		}
		return v, nil
	}
	if v := os.Getenv("HTMLUP_PASSWORD"); v != "" {
		return v, nil
	}
	return "", nil
}

func SelectedProfile(cmd *cobra.Command, providerName, profileName string) (htmlconfig.Profile, string, error) {
	cfg, err := htmlconfig.Load(htmlconfig.PathFromCommand(cmd))
	if err != nil {
		return nil, "", err
	}
	return cfg.ProviderDefault(providerName, profileName)
}

func PublishConfigured(ctx context.Context, files fs.FS, cfg htmlconfig.Config, dryRun, verbose bool, out *ui.Output) (Result, error) {
	providerName, _, profile, ok := cfg.PublishDefault()
	if !ok {
		return Result{}, fmt.Errorf("config default must be set to provider.profile before using htmlup publish")
	}
	p, ok := Get(providerName)
	if !ok {
		return Result{}, fmt.Errorf("unknown provider %q in config default", providerName)
	}
	return p.Publish(ctx, files, profile, dryRun, verbose, out)
}

func FlagChanged(cmd *cobra.Command, name string) bool {
	return cmd != nil && cmd.Flags().Changed(name)
}
