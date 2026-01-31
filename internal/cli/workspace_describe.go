package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// workspaceDescribeCmd shows detailed information about a workspace.
var workspaceDescribeCmd = &cobra.Command{
	Use:   "describe <workspace-id>",
	Short: "Show workspace details",
	Long:  `Display detailed information about a workspace.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceID := args[0]

		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		result, err := eng.DescribeWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		PrintSection("Workspace Details")

		PrintLabelValue("Workspace ID", result.WorkspaceID)
		PrintLabelValue("Workspace Path", result.WorkspacePath)
		PrintLabelValue("Repo", result.Repo)
		PrintLabelValue("Applied", fmt.Sprintf("%t", result.Applied))
		PrintLabelValue("Mode", result.Mode)
		PrintLabelValue("Active Store", result.ActiveStore)

		if len(result.Stack) > 0 {
			PrintSubsection(fmt.Sprintf("\nStack (%s)", PrintCount(len(result.Stack), "store", "stores")))
			PrintNumberedList(result.Stack, 1)
		} else {
			PrintSubsection("\nStack")
			PrintEmptyState("Stack is empty")
		}

		if len(result.AppliedStores) > 0 {
			PrintSubsection(fmt.Sprintf("\nApplied Stores (%s)", PrintCount(len(result.AppliedStores), "store", "stores")))
			storesList := make([]string, 0, len(result.AppliedStores))
			for _, store := range result.AppliedStores {
				storesList = append(storesList, fmt.Sprintf("%s (%s)", store.Store, store.Type))
			}
			PrintList(storesList, 1)
		}

		if len(result.Paths) > 0 {
			PrintSubsection(fmt.Sprintf("\nApplied Paths (%s)", PrintCount(len(result.Paths), "path", "paths")))
			pathsList := make([]string, 0, len(result.Paths))
			for path, ownership := range result.Paths {
				pathsList = append(pathsList, fmt.Sprintf("%s (from %s, %s)", path, ownership.Store, ownership.Type))
			}
			PrintList(pathsList, 1)
		} else {
			PrintSubsection("\nApplied Paths")
			PrintEmptyState("No paths applied")
		}

		return nil
	},
}
