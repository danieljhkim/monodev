package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

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
			ShowContent: diffPatch || (!diffNameOnly && !diffNameStatus),
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
	for _, file := range changedFiles(result) {
		fmt.Println(file.Path)
	}
	return nil
}

// formatNameStatus outputs filenames with status indicators (M, A, D).
func formatNameStatus(result *engine.DiffResult) error {
	for _, file := range changedFiles(result) {
		statusChar := getStatusChar(file.Status)
		switch file.Status {
		case "added":
			_, _ = successColor.Printf("%s\t%s\n", statusChar, file.Path)
		case "removed":
			_, _ = errorColor.Printf("%s\t%s\n", statusChar, file.Path)
		case "modified":
			_, _ = warningColor.Printf("%s\t%s\n", statusChar, file.Path)
		default:
			fmt.Printf("%s\t%s\n", statusChar, file.Path)
		}
	}
	return nil
}

// formatDefaultDiff outputs a git-like unified patch plus a change summary.
func formatDefaultDiff(result *engine.DiffResult) error {
	initColors()

	files := changedFiles(result)
	if len(files) == 0 {
		PrintEmptyState("No changes detected")
		return nil
	}

	// Compact header: store and workspace on one line
	fmt.Println()
	_, _ = dimColor.Printf("  store: ")
	_, _ = infoColor.Printf("%s", result.StoreID)
	_, _ = dimColor.Printf("  workspace: ")
	_, _ = infoColor.Printf("%s\n", result.WorkspaceID)

	insertions := 0
	deletions := 0

	for _, file := range files {
		fmt.Println()
		printDiffFileHeader(file)

		if file.UnifiedDiff != "" {
			printUnifiedDiff(file.UnifiedDiff)
		}

		insertions += file.Additions
		deletions += file.Deletions
	}

	// Color-coded summary line
	fmt.Println()
	_, _ = dimColor.Print("  ")
	fmt.Printf("%d file%s changed", len(files), plural(len(files)))
	if insertions > 0 {
		_, _ = successColor.Printf(", %d insertion%s(+)", insertions, plural(insertions))
	}
	if deletions > 0 {
		_, _ = errorColor.Printf(", %d deletion%s(-)", deletions, plural(deletions))
	}
	fmt.Println()

	return nil
}

func changedFiles(result *engine.DiffResult) []engine.DiffFileInfo {
	files := make([]engine.DiffFileInfo, 0, len(result.Files))
	for _, file := range result.Files {
		if file.Status != "unchanged" {
			files = append(files, file)
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
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

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func printDiffFileHeader(file engine.DiffFileInfo) {
	statusChar := getStatusChar(file.Status)
	statusClr := dimColor
	switch file.Status {
	case "added":
		statusClr = successColor
	case "removed":
		statusClr = errorColor
	case "modified":
		statusClr = warningColor
	}

	// Status badge + file path + stats
	_, _ = statusClr.Printf("  %s ", statusChar)
	_, _ = headerColor.Printf("%s", file.Path)

	// Insertion/deletion counts, colored individually
	if file.Additions > 0 {
		_, _ = successColor.Printf("  +%d", file.Additions)
	}
	if file.Deletions > 0 {
		_, _ = errorColor.Printf("  -%d", file.Deletions)
	}
	fmt.Println()

	// Thin separator under the file header
	_, _ = dimColor.Println("  " + strings.Repeat("─", 50))
}

func printUnifiedDiff(diffText string) {
	lines := strings.Split(diffText, "\n")
	for i, line := range lines {
		// Preserve trailing newline semantics from generated patches.
		if i == len(lines)-1 && line == "" {
			continue
		}

		switch {
		// Skip redundant diff header lines — already shown in file header
		case strings.HasPrefix(line, "diff --git "),
			strings.HasPrefix(line, "+++ "),
			strings.HasPrefix(line, "--- "):
			continue
		case strings.HasPrefix(line, "@@"):
			_, _ = infoColor.Printf("  %s\n", line)
		case strings.HasPrefix(line, "+"):
			_, _ = successColor.Printf("  %s\n", line)
		case strings.HasPrefix(line, "-"):
			_, _ = errorColor.Printf("  %s\n", line)
		default:
			fmt.Printf("  %s\n", line)
		}
	}
}
