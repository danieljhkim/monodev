package cli

import (
	"github.com/spf13/cobra"
)

var (
	// Global flags
	jsonOutput bool
)

// rootCmd is the root command for monodev.
var rootCmd = &cobra.Command{
	Use:   "monodev",
	Short: "Component-scoped development overlay manager",
	Long: `monodev manages component-specific development overlays for large monorepos.

It lets you persist, re-apply, and manage dev-only files (Makefiles, IDE config, scripts)
without polluting git history.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Add all subcommands
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(unapplyCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(useCmd)
	rootCmd.AddCommand(trackCmd)
	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(describeCmd)
}

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}
