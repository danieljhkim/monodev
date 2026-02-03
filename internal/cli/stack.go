package cli

import (
	"github.com/spf13/cobra"
)

// stackCmd is the parent command for stack management.
var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage the store stack",
	Long: `Manage the persistent store stack for the current repository.

The store stack determines which stores are applied when running 'monodev stack apply'.
Stores are applied in order, with later stores taking precedence on path conflicts.`,
}

func init() {
	stackCmd.AddCommand(stackLsCmd)
	stackCmd.AddCommand(stackAddCmd)
	stackCmd.AddCommand(stackPopCmd)
	stackCmd.AddCommand(stackClearCmd)
	stackCmd.AddCommand(stackApplyCmd)
	stackCmd.AddCommand(stackUnapplyCmd)

	// Flags for stack apply
	stackApplyCmd.Flags().BoolP("force", "f", false, "Force apply, overwriting conflicts")
	stackApplyCmd.Flags().Bool("dry-run", false, "Show what would be applied without making changes")
	// Flags for stack unapply
	stackUnapplyCmd.Flags().BoolP("force", "f", false, "Force removal even if validation fails")
	stackUnapplyCmd.Flags().Bool("dry-run", false, "Show what would be removed without making changes")
}
