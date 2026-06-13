package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlupclaude/internal/provider"

	_ "github.com/truehhart/htmlupclaude/internal/provider/github"
	_ "github.com/truehhart/htmlupclaude/internal/provider/s3"
)

func main() {
	root := &cobra.Command{
		Use:   "htmlup",
		Short: "Upload HTML pages and make them publicly available",
	}
	for _, p := range provider.All() {
		root.AddCommand(p.Command())
	}
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
