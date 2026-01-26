package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workspace status",
	Long:  `Display the current state of overlays in the workspace.`,
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

		req := &engine.StatusRequest{
			CWD: cwd,
		}

		result, err := eng.Status(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		// Active Workspace Section
		PrintSection("Active Workspace")
		PrintLabelValue("Repo Fingerprint", result.RepoFingerprint)
		PrintLabelValue("Workspace Path", result.WorkspacePath)

		modeDisplay := result.Mode
		if modeDisplay == "" {
			modeDisplay = "Not Applied"
		}
		PrintLabelValue("Mode", modeDisplay)

		// Applied Stores Table
		if len(result.AppliedStoreDetails) > 0 {
			fmt.Println()
			PrintSubsection("Applied Stores:")

			headers := []string{"storeId", "path", "mode"}
			rows := [][]string{}

			for key, store := range result.Paths {
				rows = append(rows, []string{
					store.Store,
					key,
					store.Type,
				})
			}

			PrintTable(headers, rows)
		}

		// Stack Display
		if len(result.Stack) > 0 {
			fmt.Println()
			stackDisplay := fmt.Sprintf("[\"%s\"]", strings.Join(result.Stack, "\", \""))
			PrintLabelValue("Stack", stackDisplay)
		} else {
			fmt.Println()
			PrintLabelValue("Stack", "[]")
		}

		// Separator
		PrintSeparator()

		// Active Store Section
		PrintSection("Active Store")

		activeStoreDisplay := result.ActiveStore
		if activeStoreDisplay == "" {
			activeStoreDisplay = "(none)"
		}
		PrintLabelValue("Store ID", activeStoreDisplay)

		// Color-code status
		statusColor := dimColor
		switch result.ActiveStoreStatus {
		case "Applied":
			statusColor = successColor
		case "Partial":
			statusColor = warningColor
		case "Not Applied":
			statusColor = dimColor
		}
		PrintLabelValueWithColor("Status", result.ActiveStoreStatus, statusColor)

		// Tracked Paths Table
		if len(result.TrackedPathDetails) > 0 {
			fmt.Println()
			PrintSubsection("Tracked Paths:")

			headers := []string{"path", "applied?", "saved?"}
			rows := [][]string{}

			for _, tp := range result.TrackedPathDetails {
				appliedMark := " "
				if tp.IsApplied {
					appliedMark = "✓"
				}

				savedMark := " "
				if tp.IsSaved {
					savedMark = "✓"
				}

				rows = append(rows, []string{tp.Path, appliedMark, savedMark})
			}

			PrintTable(headers, rows)
		} else if result.ActiveStore != "" {
			fmt.Println()
			PrintEmptyState("No tracked paths in active store")
		}

		return nil
	},
}
