package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/buildinfo"
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

	root := &cobra.Command{
		Use:     "htmlup",
		Short:   "Upload HTML pages and make them publicly available",
		Version: info.String(),
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(newVersionCmd(info))
	for _, p := range provider.All() {
		root.AddCommand(p.Command())
	}
	if err := root.Execute(); err != nil {
		os.Exit(1)
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
