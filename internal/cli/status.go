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

		PrintInfo(fmt.Sprintf("Workspace: %s", result.WorkspacePath))
		PrintInfo(fmt.Sprintf("Active Store: %s", result.ActiveStore))
		PrintInfo(fmt.Sprintf("Applied: %v", result.Applied))
		if result.Applied {
			PrintInfo(fmt.Sprintf("Mode: %s", result.Mode))
			PrintInfo(fmt.Sprintf("Applied Paths: %d", len(result.Paths)))
		}
		PrintInfo(fmt.Sprintf("Tracked Paths: %d", len(result.TrackedPaths)))
		if len(result.TrackedPaths) > 0 {
			PrintInfo("\nTracked in active store:")
			for _, path := range result.TrackedPaths {
				PrintInfo(fmt.Sprintf("  %s", path))
			}
		}
		return nil
	},
}
