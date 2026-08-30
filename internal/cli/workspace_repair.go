package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/spf13/cobra"
)

var (
	workspaceRepairRebind string
	workspaceRepairForce  bool
)

var workspaceRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "List or rebind orphaned workspaces for the current repo",
	Long: `List workspace state files that belong to the current repository but
are stored under a fingerprint that no longer matches, and optionally rebind
one onto the current identity.

Orphans appear after a remote URL change, a clone move under the old identity
scheme, or a repo-id migration. Rebinding restores the active store and the
applied-overlay ledger under the current workspace ID.`,
	Args: cobra.NoArgs,
	RunE: runWorkspaceRepair,
}

func init() {
	workspaceRepairCmd.Flags().StringVar(&workspaceRepairRebind, "rebind", "", "Workspace ID to rebind onto the current repository identity")
	workspaceRepairCmd.Flags().BoolVarP(&workspaceRepairForce, "force", "f", false, "Overwrite an existing workspace at the rebound ID")
}

func runWorkspaceRepair(cmd *cobra.Command, args []string) error {
	eng, err := newEngine()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	ctx := context.Background()

	if workspaceRepairRebind != "" {
		result, err := eng.RebindWorkspace(ctx, &engine.RebindWorkspaceRequest{
			CWD:         cwd,
			WorkspaceID: workspaceRepairRebind,
			Force:       workspaceRepairForce,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return outputJSON(result)
		}
		PrintSection("Rebind Workspace")
		PrintSuccess(fmt.Sprintf("Rebound %s → %s", result.OldWorkspaceID, result.NewWorkspaceID))
		PrintLabelValue("Workspace Path", result.WorkspacePath)
		PrintLabelValue("Active Store", result.ActiveStore)
		PrintLabelValue("Applied", fmt.Sprintf("%t", result.Applied))
		PrintLabelValue("Applied Paths", fmt.Sprintf("%d", result.AppliedPaths))
		return nil
	}

	result, err := eng.ListOrphanedWorkspaces(ctx, cwd)
	if err != nil {
		return err
	}
	if jsonOutput {
		return outputJSON(result)
	}

	PrintSection("Orphaned Workspaces")
	if len(result.Orphans) == 0 {
		PrintEmptyState("No orphaned workspaces for this repository")
		return nil
	}

	rows := make([][]string, 0, len(result.Orphans))
	for _, orphan := range result.Orphans {
		appliedMark := " "
		if orphan.Applied {
			appliedMark = "✓"
		}
		rows = append(rows, []string{
			orphan.WorkspaceID,
			orphan.CurrentID,
			orphan.WorkspacePath,
			orphan.ActiveStore,
			appliedMark,
			fmt.Sprintf("%d", orphan.AppliedPathCount),
		})
	}
	PrintTable([]string{"Orphan ID", "Current ID", "Path", "Active Store", "Applied", "Paths"}, rows)
	fmt.Println()
	PrintInfo("Rebind one with: monodev workspace repair --rebind <orphan-id>")
	return nil
}
