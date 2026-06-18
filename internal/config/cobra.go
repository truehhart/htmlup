package config

import "github.com/spf13/cobra"

func PathFromCommand(cmd *cobra.Command) string {
	flag := cmd.Root().PersistentFlags().Lookup("config")
	if flag == nil {
		return ""
	}
	return flag.Value.String()
}
