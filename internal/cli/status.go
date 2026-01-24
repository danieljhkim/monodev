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

		fmt.Printf("Workspace: %s\n", result.WorkspacePath)
		fmt.Printf("Active Store: %s\n", result.ActiveStore)
		fmt.Printf("Applied: %v\n", result.Applied)
		if result.Applied {
			fmt.Printf("Mode: %s\n", result.Mode)
			fmt.Printf("Applied Paths: %d\n", len(result.Paths))
		}
		fmt.Printf("Tracked Paths: %d\n", len(result.TrackedPaths))
		if len(result.TrackedPaths) > 0 {
			fmt.Printf("\nTracked in active store:\n")
			for _, path := range result.TrackedPaths {
				fmt.Printf("  %s\n", path)
			}
		}
		return nil
	},
}
