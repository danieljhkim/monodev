package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// workspaceLsCmd lists all workspaces.
var workspaceLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all workspaces",
	Long:  `Display all workspaces with their current state.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		result, err := eng.ListWorkspaces(ctx)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		if len(result.Workspaces) == 0 {
			PrintSection("Workspaces")
			PrintEmptyState("No workspaces found")
			return nil
		}

		PrintSection("Workspaces")
		rows := make([][]string, 0, len(result.Workspaces))
		for _, ws := range result.Workspaces {
			appliedMark := " "
			if ws.Applied {
				appliedMark = "✓"
			}
			rows = append(rows, []string{
				ws.WorkspaceID,
				ws.WorkspacePath,
				ws.ActiveStore,
				appliedMark,
				fmt.Sprintf("%d", ws.AppliedPathCount),
			})
		}
		PrintTable([]string{"Workspace ID", "Workspace Path", "Active Store", "Applied", "Paths"}, rows)
		return nil
	},
}
