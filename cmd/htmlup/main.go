package main

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/buildinfo"
	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/provider"

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
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create an empty config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			path := htmlconfig.PathFromCommand(cmd)
			if path == "" {
				var err error
				path, err = htmlconfig.DefaultPath()
				if err != nil {
					return err
				}
			}
			if !force {
				if _, err := os.Stat(path); err == nil {
					return errors.New("config already exists at " + path + " (use --force to overwrite)")
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if err := htmlconfig.Save(path, htmlconfig.Empty()); err != nil {
				return err
			}
			cmd.Printf("created %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")
	return cmd
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
