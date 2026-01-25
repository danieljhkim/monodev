package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var untrackCmd = &cobra.Command{
	Use:   "untrack <path>...",
	Short: "Stop tracking paths in the active store",
	Long: `Remove paths from the active store's track file.

This command does NOT:
- Modify workspace files
- Delete store overlay content (use 'prune' for that)

It only removes the paths from track.json metadata.`,
	Args: cobra.MinimumNArgs(1),
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

		req := &engine.UntrackRequest{
			CWD:   cwd,
			Paths: args,
		}

		if err := eng.Untrack(ctx, req); err != nil {
			return err
		}

		fmt.Printf("Untracked %d path(s)\n", len(args))
		fmt.Println("Note: Store overlay content not deleted. Use 'monodev prune' to clean up.")
		return nil
	},
}
