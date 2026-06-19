package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/buildinfo"
	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/provider"
	"github.com/truehhart/htmlup/internal/ui"
	"github.com/truehhart/htmlup/internal/wizard"

	_ "github.com/truehhart/htmlup/internal/provider/github"
	_ "github.com/truehhart/htmlup/internal/provider/s3"
)

// Stamped in at release time via -ldflags (see .goreleaser.yaml). For non-release
// builds these stay at their defaults and buildinfo backfills them from the VCS
// metadata the Go toolchain embeds in the binary.
var (
	version = "dev"
	commit  string
	date    string
)

func main() {
	info := buildinfo.Resolve(version, commit, date)
	configPath := ""

	root := &cobra.Command{
		Use:     "htmlup",
		Short:   "Upload HTML pages and make them publicly available",
		Version: info.String(),
	}
	root.SetVersionTemplate("{{.Version}}\n")
	// We render errors ourselves through ui (lowercase, consistent voice) instead
	// of cobra's "Error: …" line, and a user-cancelled prompt exits quietly.
	root.SilenceErrors = true
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path (default: ~/.htmlup/config.toml)")
	root.AddCommand(newVersionCmd(info))
	root.AddCommand(newConfigCmd())

	// `publish` runs the configured default profile on its own; each provider
	// hangs a flag-driven subcommand (`publish github`, `publish s3`) under it.
	// `setup` collects the optional bootstrap subcommand of any provider that
	// has one (currently just github).
	publishCmd := newPublishCmd()
	setupCmd := newSetupCmd()
	for _, p := range provider.All() {
		publishCmd.AddCommand(p.PublishCommand())
		if s, ok := p.(provider.Setupper); ok {
			setupCmd.AddCommand(s.SetupCommand())
		}
	}
	root.AddCommand(publishCmd)
	if setupCmd.HasSubCommands() {
		root.AddCommand(setupCmd)
	}
	err := root.Execute()
	if err == nil {
		return
	}
	if errors.Is(err, ui.ErrAborted) {
		os.Exit(130) // 128 + SIGINT, the conventional code for an interrupted run
	}
	ui.Auto().Error(err)
	os.Exit(1)
}

func newPublishCmd() *cobra.Command {
	var (
		dryRun  bool
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "publish <path>",
		Short: "Publish using the configured default provider profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			out := ui.Auto()
			cfg, err := htmlconfig.Load(htmlconfig.PathFromCommand(cmd))
			if err != nil {
				return err
			}
			result, err := provider.PublishConfigured(cmd.Context(), args[0], cfg, dryRun, verbose, out)
			if err != nil {
				return err
			}
			out.Result(result.URLs...)
			return nil
		},
	}
	// Persistent so the provider subcommands (publish github/s3) inherit the
	// exact same flag — a single backing value, regardless of whether --dry-run
	// is given before or after the provider name. Avoids a shadowed duplicate
	// per subcommand silently diverging (e.g. a dry run becoming a real publish).
	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "show what would be uploaded without writing")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "per-file progress and SDK detail")
	return cmd
}

// newSetupCmd is the parent for provider bootstrap commands; providers attach
// their own subcommand (e.g. `setup github`). With no subcommand it prints help.
func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup <provider>",
		Short: "Bootstrap a provider's target for use with htmlup",
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage htmlup config profiles",
	}
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigInspectCmd())
	cmd.AddCommand(newConfigSetCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var (
		force          bool
		nonInteractive bool
		providerName   string
		profileName    string
		setFlag        []string
		setDefaultStr  string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up a config profile (interactive by default)",
		Long: `Walks through provider selection and the fields that provider needs,
writing the result to the config file. The same fields can be supplied
non-interactively via --set key=value flags; use --non-interactive to
fail rather than prompt for anything missing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			preset, err := parseSetFlags(setFlag)
			if err != nil {
				return err
			}
			setDefault, err := parseTristate(setDefaultStr)
			if err != nil {
				return fmt.Errorf("--set-default: %w", err)
			}

			out := ui.Auto()

			// Non-TTY stdin (pipe, CI) implies non-interactive — there's no one
			// to answer prompts. Flags + --set still flow through normally.
			interactive := !nonInteractive && ui.DetectTTY(cmd.InOrStdin())
			var prompter *ui.Prompter
			if interactive {
				prompter = out.Prompter(cmd.InOrStdin())
			}

			result, err := wizard.Run(wizard.Options{
				Path:           htmlconfig.PathFromCommand(cmd),
				ProviderName:   providerName,
				ProfileName:    profileName,
				Preset:         preset,
				NonInteractive: !interactive,
				Force:          force,
				SetDefault:     setDefault,
				Prompter:       prompter,
			})
			if err != nil {
				return err
			}

			verb := "updated"
			if result.IsNew {
				verb = "created"
			}
			out.Success("%s profile %s.%s in %s", verb, result.Provider, result.Profile, result.Path)
			if result.Default {
				out.Info("set as the default profile")
			}
			out.Next("htmlup publish <path>")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing profile without confirming")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "never prompt; require all values via flags")
	cmd.Flags().StringVar(&providerName, "provider", "", "provider name (e.g. github, s3)")
	cmd.Flags().StringVar(&profileName, "profile", "", "profile name (default: 'default')")
	cmd.Flags().StringArrayVar(&setFlag, "set", nil, "preset a field: --set key=value (repeatable)")
	cmd.Flags().StringVar(&setDefaultStr, "set-default", "", "force whether to set this as default: yes|no (default: ask if interactive)")
	return cmd
}

func parseSetFlags(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--set %q: expected key=value", p)
		}
		out[k] = v
	}
	return out, nil
}

func parseTristate(s string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return nil, nil
	case "y", "yes", "true", "1":
		t := true
		return &t, nil
	case "n", "no", "false", "0":
		f := false
		return &f, nil
	}
	return nil, fmt.Errorf("expected yes|no, got %q", s)
}

func newConfigInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Print the effective config file contents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			cfg, err := htmlconfig.Load(htmlconfig.PathFromCommand(cmd))
			if err != nil {
				return err
			}
			ui.Auto().Plain(cfg.TOML())
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			path := htmlconfig.PathFromCommand(cmd)
			cfg, err := htmlconfig.Load(path)
			if err != nil {
				return err
			}
			cfg, err = htmlconfig.Set(cfg, args[0], args[1])
			if err != nil {
				return err
			}
			if err := htmlconfig.Save(path, cfg); err != nil {
				return err
			}
			return nil
		},
	}
}

func newVersionCmd(info buildinfo.Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			ui.Auto().Result(info.String())
		},
	}
}
