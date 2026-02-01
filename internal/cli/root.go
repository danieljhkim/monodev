package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	jsonOutput bool
)

// rootCmd is the root command for monodev.
var rootCmd = &cobra.Command{
	Use:     "monodev",
	Version: "dev",
	Short:   "Component-scoped development overlay manager",
	Long: `monodev manages component-specific development overlays for large monorepos.

It lets you persist, re-apply, and manage dev-only files (Makefiles, IDE config, scripts)
without polluting git history.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func SetVersion(v string) {
	if v == "" {
		return
	}
	rootCmd.Version = v
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the monodev CLI version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(os.Stdout, rootCmd.Version)
		},
	})

	// Add all subcommands
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(unapplyCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(checkoutCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(trackCmd)
	rootCmd.AddCommand(untrackCmd)
	rootCmd.AddCommand(storeCmd)
	rootCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(stackCmd)
	rootCmd.AddCommand(diffCmd)
}

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}
