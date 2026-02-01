package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	diffStoreID    string
	diffPatch      bool
	diffNameOnly   bool
	diffNameStatus bool
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show differences between store overlay and workspace",
	Long:  `Display which tracked files have been modified, added, or removed compared to the store overlay.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		req := &engine.DiffRequest{
			CWD:         cwd,
			StoreID:     diffStoreID,
			ShowContent: diffPatch,
			NameOnly:    diffNameOnly,
			NameStatus:  diffNameStatus,
		}

		result, err := eng.Diff(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		return formatDiffOutput(result)
	},
}

func init() {
	diffCmd.Flags().StringVarP(&diffStoreID, "store-id", "s", "", "Store to diff against (default: active store)")
	diffCmd.Flags().BoolVarP(&diffPatch, "patch", "p", false, "Show unified diff content")
	diffCmd.Flags().BoolVar(&diffNameOnly, "name-only", false, "Show only file names")
	diffCmd.Flags().BoolVar(&diffNameStatus, "name-status", false, "Show file names with status")
}

// formatDiffOutput formats the diff result for display.
func formatDiffOutput(result *engine.DiffResult) error {
	// Handle special output formats
	if diffNameOnly {
		return formatNameOnly(result)
	}

	if diffNameStatus {
		return formatNameStatus(result)
	}

	// Default format
	return formatDefaultDiff(result)
}

// formatNameOnly outputs only filenames (no status indicators).
func formatNameOnly(result *engine.DiffResult) error {
	for _, file := range result.Files {
		if file.Status != "unchanged" {
			fmt.Println(file.Path)
		}
	}
	return nil
}

// formatNameStatus outputs filenames with status indicators (M, A, D).
func formatNameStatus(result *engine.DiffResult) error {
	for _, file := range result.Files {
		if file.Status != "unchanged" {
			statusChar := getStatusChar(file.Status)
			fmt.Printf("%s\t%s\n", statusChar, file.Path)
		}
	}
	return nil
}

// formatDefaultDiff outputs the default diff format with summary and details.
func formatDefaultDiff(result *engine.DiffResult) error {
	PrintSection("Diff Summary")
	PrintLabelValue("Store ID", result.StoreID)
	PrintLabelValue("Workspace ID", result.WorkspaceID)

	// Count files by status
	modified := 0
	added := 0
	removed := 0
	unchanged := 0

	for _, file := range result.Files {
		switch file.Status {
		case "modified":
			modified++
		case "added":
			added++
		case "removed":
			removed++
		case "unchanged":
			unchanged++
		}
	}

	fmt.Println()
	PrintLabelValue("Modified", fmt.Sprintf("%d", modified))
	PrintLabelValue("Added", fmt.Sprintf("%d", added))
	PrintLabelValue("Removed", fmt.Sprintf("%d", removed))
	PrintLabelValue("Unchanged", fmt.Sprintf("%d", unchanged))

	// Show changed files only
	changedFiles := []engine.DiffFileInfo{}
	for _, file := range result.Files {
		if file.Status != "unchanged" {
			changedFiles = append(changedFiles, file)
		}
	}

	PrintSection("Changed Files")

	if len(changedFiles) == 0 {
		PrintEmptyState("No changes detected")
		return nil
	}

	for i, file := range changedFiles {
		if i > 0 {
			fmt.Println()
		}

		PrintLabelValue("Status", file.Status)
		PrintLabelValue("Path", file.Path)

		if file.IsDir {
			PrintLabelValue("Type", "directory")
		} else {
			if file.WorkspaceHash != "" {
				PrintLabelValue("Workspace Hash", truncateHash(file.WorkspaceHash))
			}
			if file.StoreHash != "" {
				PrintLabelValue("Store Hash", truncateHash(file.StoreHash))
			}
		}
	}

	return nil
}

// getStatusChar returns the single-character status indicator.
func getStatusChar(status string) string {
	switch status {
	case "modified":
		return "M"
	case "added":
		return "A"
	case "removed":
		return "D"
	case "unchanged":
		return "U"
	default:
		return "?"
	}
}

// truncateHash truncates a hash to the first 8 characters for display.
func truncateHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
