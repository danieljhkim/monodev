package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var clearCmd = &cobra.Command{
	Use:    "clear",
	Short:  "Removed; use workspace rm",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("clear has been removed; use 'monodev workspace rm' to delete the current workspace")
	},
}
