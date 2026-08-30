package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stackCmd = &cobra.Command{
	Use:    "stack",
	Short:  "Removed; use apply and unapply",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("stack has been removed; use 'monodev apply [store-id...]' and 'monodev unapply [store-id...]' instead")
	},
}

var stackAddCmd = &cobra.Command{
	Use:    "add [store-id]",
	Short:  "Removed; use apply",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("stack add has been removed; use 'monodev apply %s' instead", args[0])
		}
		return fmt.Errorf("stack add has been removed; use 'monodev apply [store-id...]' instead")
	},
}

var stackApplyCmd = &cobra.Command{
	Use:    "apply",
	Short:  "Removed; use apply",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("stack apply has been removed; use 'monodev apply [store-id...]' instead")
	},
}

var stackUnapplyCmd = &cobra.Command{
	Use:    "unapply",
	Short:  "Removed; use unapply",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("stack unapply has been removed; use 'monodev unapply [store-id...]' instead")
	},
}

var stackLsCmd = &cobra.Command{
	Use:    "ls",
	Short:  "Removed; use status",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("stack ls has been removed; use 'monodev status' to list applied stores")
	},
}

var stackPopCmd = &cobra.Command{
	Use:    "pop",
	Short:  "Removed; use unapply",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("stack pop has been removed; use 'monodev unapply [store-id...]' instead")
	},
}

var stackClearCmd = &cobra.Command{
	Use:    "clear",
	Short:  "Removed; use unapply",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("stack clear has been removed; use 'monodev unapply [store-id...]' instead")
	},
}

func init() {
	stackCmd.AddCommand(stackLsCmd)
	stackCmd.AddCommand(stackAddCmd)
	stackCmd.AddCommand(stackPopCmd)
	stackCmd.AddCommand(stackClearCmd)
	stackCmd.AddCommand(stackApplyCmd)
	stackCmd.AddCommand(stackUnapplyCmd)
}
