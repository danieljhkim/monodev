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
	clearForce  bool
	clearDryRun bool
)

// clearCmd deletes the current workspace state file.
var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the current workspace",
	Long: `Delete the current workspace state file permanently.

This command auto-discovers the current workspace and deletes it.
If the workspace has applied overlays, you'll need to use --force to proceed.

IMPORTANT: This only deletes the state file, not the actual workspace files.
Use 'monodev unapply' first to remove applied overlays from the workspace.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		// Get current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Discover workspace
		_, repoFingerprint, workspacePath, err := eng.DiscoverWorkspace(cwd)
		if err != nil {
			return err
		}

		// Compute workspace ID
		workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

		req := &engine.DeleteWorkspaceRequest{
			WorkspaceID: workspaceID,
			Force:       clearForce,
			DryRun:      clearDryRun,
		}

		result, err := eng.DeleteWorkspace(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		// Handle dry-run output
		if clearDryRun {
			PrintSection("Dry Run: Clear Workspace")
			PrintInfo(fmt.Sprintf("Workspace ID: %s", result.WorkspaceID))
			PrintInfo(fmt.Sprintf("Workspace Path: %s", result.WorkspacePath))
			if result.PathsRemoved > 0 {
				PrintInfo(fmt.Sprintf("Applied Paths: %d", result.PathsRemoved))
			}
			fmt.Println()
			PrintWarning("Run without --dry-run to clear")
			return nil
		}

		// Success output
		PrintSection("Clear Workspace")
		PrintSuccess(fmt.Sprintf("Cleared workspace: %s", result.WorkspacePath))

		return nil
	},
}

func init() {
	clearCmd.Flags().BoolVarP(&clearForce, "force", "f", false, "Force deletion even if workspace has applied paths")
	clearCmd.Flags().BoolVar(&clearDryRun, "dry-run", false, "Show what would be deleted without deleting")
}
