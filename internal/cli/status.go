package cli

import (
	"context"
	"fmt"
	"os"

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

		PrintSection("Workspace Status")

		PrintLabelValue("Workspace", result.WorkspacePath)
		PrintLabelValue("Active Store", result.ActiveStore)

		if result.Applied {
			PrintLabelValueWithColor("Status", "Applied", successColor)
			PrintLabelValue("Mode", result.Mode)
			PrintLabelValue("Applied Paths", PrintCount(len(result.Paths), "path", "paths"))
		} else {
			PrintLabelValueWithColor("Status", "Not Applied", dimColor)
		}

		PrintLabelValue("Tracked Paths", PrintCount(len(result.TrackedPaths), "path", "paths"))

		if len(result.TrackedPaths) > 0 {
			PrintSubsection("Tracked in active store:")
			PrintList(result.TrackedPaths, 1)
		}

		return nil
	},
}
