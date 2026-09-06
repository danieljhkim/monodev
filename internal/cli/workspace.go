package cli

import (
	"github.com/spf13/cobra"
)

// workspaceCmd is the parent command for workspace management.
var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage workspaces",
	Long:  `Manage workspace state files for development overlays.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	workspaceCmd.AddCommand(workspaceLsCmd)
	workspaceCmd.AddCommand(workspaceDescribeCmd)
	workspaceCmd.AddCommand(workspaceRmCmd)
	workspaceCmd.AddCommand(workspaceRepairCmd)
}
