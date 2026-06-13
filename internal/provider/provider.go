package provider

import (
	"io/fs"

	"github.com/spf13/cobra"
)

type Target struct {
	Files   fs.FS
	DryRun  bool
	Verbose bool
}

type Result struct {
	URL string
}

type Provider interface {
	Name() string
	Command() *cobra.Command
}
