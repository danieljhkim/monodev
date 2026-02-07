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
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
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

	// Define command groups
	rootCmd.AddGroup(&cobra.Group{
		ID:    "workspace-lifecycle",
		Title: "Workspace Lifecycle:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "store-operations",
		Title: "Store Operations:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "stack-management",
		Title: "Stack Management:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "workspace-management",
		Title: "Workspace Management:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "remote-persistence",
		Title: "Remote Persistence:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "cli-tooling",
		Title: "CLI & Tooling:",
	})

	// CLI & Tooling commands
	versionCmd := &cobra.Command{
		Use:     "version",
		Short:   "Print the monodev CLI version",
		Args:    cobra.NoArgs,
		GroupID: "cli-tooling",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(os.Stdout, rootCmd.Version)
		},
	}
	rootCmd.AddCommand(versionCmd)

	// Add help command to CLI & Tooling group
	helpCmd := &cobra.Command{
		Use:     "help [command]",
		Short:   "Help about any command",
		GroupID: "cli-tooling",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Root().Help()
		},
	}
	rootCmd.SetHelpCommand(helpCmd)

	// Add completion command to CLI & Tooling group
	completionCmd := &cobra.Command{
		Use:     "completion",
		Short:   "Generate the autocompletion script for the specified shell",
		GroupID: "cli-tooling",
		Long: `Generate the autocompletion script for monodev for the specified shell.
See each sub-command's help for details on how to use the generated script.`,
	}
	completionCmd.AddCommand(&cobra.Command{
		Use:                   "bash",
		Short:                 "Generate the autocompletion script for bash",
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootCmd.GenBashCompletion(os.Stdout)
		},
	})
	completionCmd.AddCommand(&cobra.Command{
		Use:                   "zsh",
		Short:                 "Generate the autocompletion script for zsh",
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootCmd.GenZshCompletion(os.Stdout)
		},
	})
	completionCmd.AddCommand(&cobra.Command{
		Use:                   "fish",
		Short:                 "Generate the autocompletion script for fish",
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootCmd.GenFishCompletion(os.Stdout, true)
		},
	})
	completionCmd.AddCommand(&cobra.Command{
		Use:                   "powershell",
		Short:                 "Generate the autocompletion script for powershell",
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		},
	})
	rootCmd.AddCommand(completionCmd)

	// Workspace Lifecycle commands
	applyCmd.GroupID = "workspace-lifecycle"
	unapplyCmd.GroupID = "workspace-lifecycle"
	diffCmd.GroupID = "workspace-lifecycle"
	statusCmd.GroupID = "workspace-lifecycle"
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(unapplyCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(statusCmd)

	// Store Operations commands
	storeCmd.GroupID = "store-operations"
	checkoutCmd.GroupID = "store-operations"
	commitCmd.GroupID = "store-operations"
	trackCmd.GroupID = "store-operations"
	untrackCmd.GroupID = "store-operations"
	rootCmd.AddCommand(storeCmd)
	rootCmd.AddCommand(checkoutCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(trackCmd)
	rootCmd.AddCommand(untrackCmd)

	// Stack Management commands
	stackCmd.GroupID = "stack-management"
	rootCmd.AddCommand(stackCmd)

	// Workspace Management commands
	workspaceCmd.GroupID = "workspace-management"
	initCmd.GroupID = "workspace-management"
	rootCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(initCmd)

	// Remote Persistence commands
	remoteCmd.GroupID = "remote-persistence"
	pushCmd.GroupID = "remote-persistence"
	pullCmd.GroupID = "remote-persistence"
	rootCmd.AddCommand(remoteCmd)
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(pullCmd)
}

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}
