package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/spf13/cobra"
)

var (
	workspaceRmForce  bool
	workspaceRmDryRun bool
)

var workspaceRmCmd = &cobra.Command{
	Use:   "rm [workspace-id]",
	Short: "Delete a workspace state file",
	Long: `Delete a workspace state file permanently.

With no argument, deletes the current workspace. With a workspace ID, deletes
that workspace.

This command will check if the workspace has applied overlays before deletion.
If overlays are applied, you'll need to use --force to proceed.

IMPORTANT: This only deletes the state file, not the actual workspace files.
Use 'monodev unapply' first to remove applied overlays from the workspace.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		var workspaceID string
		if len(args) > 0 {
			workspaceID = args[0]
		} else {
			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				return fmt.Errorf("failed to get current directory: %w", cwdErr)
			}
			_, repoFingerprint, workspacePath, discoverErr := eng.DiscoverWorkspace(cwd)
			if discoverErr != nil {
				return discoverErr
			}
			workspaceID = state.ComputeWorkspaceID(repoFingerprint, workspacePath)
		}

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
