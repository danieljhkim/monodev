package cli

import (
	"github.com/spf13/cobra"
)

// workspaceCmd is the parent command for workspace management.
var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage workspaces",
	Long:  `Manage workspace state files for development overlays.`,
}

func init() {
	workspaceCmd.AddCommand(workspaceLsCmd)
	workspaceCmd.AddCommand(workspaceDescribeCmd)
	workspaceCmd.AddCommand(workspaceRmCmd)
}
