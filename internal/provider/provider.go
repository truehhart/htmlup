package provider

import (
	"context"
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/ui"
)

type Target struct {
	Files   fs.FS
	DryRun  bool
	Verbose bool
	// UI is the sink for all human status a provider emits (progress, dry-run
	// previews, warnings). It is never nil for a real command; provider code
	// must route user-facing text through it rather than os.Stderr.
	UI *ui.Output
}

type Result struct {
	// URLs are the served URLs of the published files, one per file in upload
	// order. A single-file publish yields exactly one URL. The command layer
	// writes these to stdout via ui.Output.URLs — the scriptable output.
	URLs []string
}

// Provider is a publish backend. Every backend supports the same two publish
// paths — flag-driven (PublishCommand, the `htmlup publish <name>` subcommand)
// and config-driven (Publish, dispatched by bare `htmlup publish`) — and
// declares the profile fields `htmlup config init` prompts for (ConfigSchema).
type Provider interface {
	Name() string
	ConfigSchema() []ConfigField
	PublishCommand() *cobra.Command
	Publish(ctx context.Context, localPath string, profile config.Profile, dryRun, verbose bool, out *ui.Output) (Result, error)
}

// Setupper is an optional capability: a backend that bootstraps its target
// (e.g. GitHub Pages) exposes its `htmlup setup <name>` subcommand here.
type Setupper interface {
	SetupCommand() *cobra.Command
}

// ConfigField describes one key a provider stores in its config profile, so
// `htmlup config init` can prompt for it without baking provider-specific
// knowledge into the CLI.
type ConfigField struct {
	Key      string        // profile key, e.g. "repo"
	Label    string        // prompt label, e.g. "Repository (owner/name)"
	Help     string        // one-line help shown above the prompt
	Required bool          // empty input is rejected
	Default  func() string // resolved default (env-derived, gh CLI, etc.); empty when unset
	Validate func(value string) error
}
