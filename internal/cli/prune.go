package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	pruneDryRun bool
	pruneForce  bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete untracked files from the active store",
	Long: `Delete overlay store content for paths that are no longer tracked.

This permanently removes files from the store's overlay directory that are
not listed in track.json.

By default, you will be prompted to confirm before deletion.
Use --force to skip the confirmation prompt.
Use --dry-run to preview what would be deleted without actually deleting.`,
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

		req := &engine.PruneRequest{
			CWD:    cwd,
			DryRun: pruneDryRun,
			Force:  pruneForce,
		}

		result, err := eng.Prune(ctx, req)
		if err != nil {
			return err
		}

		if len(result.DeletedPaths) == 0 {
			fmt.Println("No untracked files found in store.")
			return nil
		}

		if result.DryRun {
			fmt.Printf("Dry run: The following %d path(s) would be deleted from store '%s':\n", len(result.DeletedPaths), result.StoreID)
			for _, p := range result.DeletedPaths {
				fmt.Printf("  - %s\n", p)
			}
			fmt.Println("\nRun without --dry-run to actually delete these files.")
		} else {
			fmt.Printf("Successfully deleted %d untracked path(s) from store '%s'\n", len(result.DeletedPaths), result.StoreID)
		}

		return nil
	},
}

func init() {
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Preview what would be deleted without deleting")
	pruneCmd.Flags().BoolVar(&pruneForce, "force", false, "Skip confirmation prompt")
}
