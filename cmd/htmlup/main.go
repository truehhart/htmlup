package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/truehhart/htmlup/internal/buildinfo"
	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/provider"
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
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path (default: ~/.htmlup/config.toml)")
	root.AddCommand(newVersionCmd(info))
	root.AddCommand(newConfigCmd())
	root.AddCommand(newPublishCmd())
	for _, p := range provider.All() {
		root.AddCommand(p.Command())
	}
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
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
			cfg, err := htmlconfig.Load(htmlconfig.PathFromCommand(cmd))
			if err != nil {
				return err
			}
			result, err := provider.PublishConfigured(cmd.Context(), args[0], cfg, dryRun, verbose)
			if err != nil {
				return err
			}
			result.PrintURLs()
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be uploaded without writing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "per-file progress and SDK detail")
	return cmd
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

			// Non-TTY stdin (pipe, CI) implies non-interactive — there's no one
			// to answer prompts. Flags + --set still flow through normally.
			if !nonInteractive && !stdinIsTerminal() {
				nonInteractive = true
			}

			result, err := wizard.Run(wizard.Options{
				Path:           htmlconfig.PathFromCommand(cmd),
				ProviderName:   providerName,
				ProfileName:    profileName,
				Preset:         preset,
				NonInteractive: nonInteractive,
				Force:          force,
				SetDefault:     setDefault,
				Stdin:          cmd.InOrStdin(),
				Stdout:         cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}

			verb := "updated"
			if result.IsNew {
				verb = "created"
			}
			cmd.Printf("%s profile %s.%s in %s\n", verb, result.Provider, result.Profile, result.Path)
			if result.Default {
				cmd.Printf("set as default profile\n")
			}
			cmd.Printf("try: htmlup publish <path>\n")
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

func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
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
			cmd.Print(cfg.TOML())
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
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(info.String())
		},
	}
}
