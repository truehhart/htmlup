package provider

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/ui"
)

func SelectedProfile(cmd *cobra.Command, providerName, profileName string) (htmlconfig.Profile, string, error) {
	cfg, err := htmlconfig.Load(htmlconfig.PathFromCommand(cmd))
	if err != nil {
		return nil, "", err
	}
	return cfg.ProviderDefault(providerName, profileName)
}

func PublishConfigured(ctx context.Context, localPath string, cfg htmlconfig.Config, dryRun, verbose bool, out *ui.Output) (Result, error) {
	providerName, _, profile, ok := cfg.PublishDefault()
	if !ok {
		return Result{}, fmt.Errorf("config default must be set to provider.profile before using htmlup publish")
	}
	p, ok := Get(providerName)
	if !ok {
		return Result{}, fmt.Errorf("unknown provider %q in config default", providerName)
	}
	return p.Publish(ctx, localPath, profile, dryRun, verbose, out)
}

func FlagChanged(cmd *cobra.Command, name string) bool {
	return cmd != nil && cmd.Flags().Changed(name)
}
