package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	saveAll    bool
	saveDryRun bool
)

var saveCmd = &cobra.Command{
	Use:   "save [path]...",
	Short: "Save workspace files to the active store",
	Long: `Copy workspace files to the active store.

In symlink mode, only NEW paths (not already managed) are saved.
In copy mode, all specified paths are saved.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Require either paths or --all
		if len(args) == 0 && !saveAll {
			return fmt.Errorf("must specify paths to save or use --all flag")
		}

		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		req := &engine.SaveRequest{
			CWD:    cwd,
			Paths:  args,
			All:    saveAll,
			DryRun: saveDryRun,
		}

		result, err := eng.Save(ctx, req)
		if err != nil {
			return err
		}

		if saveDryRun {
			fmt.Printf("Dry run - would save %d paths\n", len(result.Saved))
			return nil
		}

		fmt.Printf("Saved %d paths\n", len(result.Saved))
		if len(result.Skipped) > 0 {
			fmt.Printf("Skipped %d paths (already managed or not tracked)\n", len(result.Skipped))
		}
		return nil
	},
}

func init() {
	saveCmd.Flags().BoolVar(&saveAll, "all", false, "Save all tracked paths")
	saveCmd.Flags().BoolVar(&saveDryRun, "dry-run", false, "Show what would be saved without saving")
}
