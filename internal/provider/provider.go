package provider

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/config"
)

type Target struct {
	Files   fs.FS
	DryRun  bool
	Verbose bool
}

type Result struct {
	// URLs are the served URLs of the published files, one per file in upload
	// order. A single-file publish yields exactly one URL.
	URLs []string
}

// PrintURLs writes each served URL on its own line to stdout. The bare,
// newline-separated list is the publish commands' machine-readable output.
func (r Result) PrintURLs() {
	for _, u := range r.URLs {
		fmt.Println(u)
	}
}

type Provider interface {
	Name() string
	Command() *cobra.Command
}

type PublishProvider interface {
	Provider
	Publish(ctx context.Context, localPath string, profile config.Profile, dryRun, verbose bool) (Result, error)
}
