package cli

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/spf13/cobra"
)

var (
	workspaceRmForce  bool
	workspaceRmDryRun bool
)

// workspaceRmCmd deletes a workspace state file.
var workspaceRmCmd = &cobra.Command{
	Use:   "rm <workspace-id>",
	Short: "Delete a workspace state file",
	Long: `Delete a workspace state file permanently.

This command will check if the workspace has applied overlays before deletion.
If overlays are applied, you'll need to use --force to proceed.

IMPORTANT: This only deletes the state file, not the actual workspace files.
Use 'monodev unapply' first to remove applied overlays from the workspace.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceID := args[0]

		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		req := &engine.DeleteWorkspaceRequest{
			WorkspaceID: workspaceID,
			Force:       workspaceRmForce,
			DryRun:      workspaceRmDryRun,
		}

		result, err := eng.DeleteWorkspace(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		// Handle dry-run output
		if workspaceRmDryRun {
			PrintSection("Dry Run: Delete Workspace")
			PrintInfo(fmt.Sprintf("Workspace ID: %s", result.WorkspaceID))
			PrintInfo(fmt.Sprintf("Workspace Path: %s", result.WorkspacePath))
			if result.PathsRemoved > 0 {
				PrintInfo(fmt.Sprintf("Applied Paths: %d", result.PathsRemoved))
			}
			fmt.Println()
			PrintWarning("Run without --dry-run to delete")
			return nil
		}

		// Success output
		PrintSection("Delete Workspace")
		PrintSuccess(fmt.Sprintf("Deleted workspace state: %s", result.WorkspaceID))
		PrintInfo(fmt.Sprintf("Workspace path: %s", result.WorkspacePath))

		return nil
	},
}

func init() {
	workspaceRmCmd.Flags().BoolVarP(&workspaceRmForce, "force", "f", false, "Force deletion even if workspace has applied paths")
	workspaceRmCmd.Flags().BoolVar(&workspaceRmDryRun, "dry-run", false, "Show what would be deleted without deleting")
}
