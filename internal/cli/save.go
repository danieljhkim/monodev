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
			PrintInfo(fmt.Sprintf("Dry run - would save %d paths", len(result.Saved)))
			if len(result.Missing) > 0 {
				PrintWarning(fmt.Sprintf("Would skip %d missing paths (not found in workspace):", len(result.Missing)))
				for _, p := range result.Missing {
					PrintWarning(fmt.Sprintf("  - %s", p))
				}
			}
			return nil
		}

		PrintSuccess(fmt.Sprintf("Saved %d paths", len(result.Saved)))
		if len(result.Skipped) > 0 {
			PrintWarning(fmt.Sprintf("Skipped %d paths (already managed or not tracked)", len(result.Skipped)))
		}
		if len(result.Missing) > 0 {
			PrintWarning(fmt.Sprintf("Missing %d paths (not found in workspace):", len(result.Missing)))
			for _, p := range result.Missing {
				PrintWarning(fmt.Sprintf("  - %s", p))
			}
		}
		return nil
	},
}

func init() {
	saveCmd.Flags().BoolVar(&saveAll, "all", false, "Save all tracked paths")
	saveCmd.Flags().BoolVar(&saveDryRun, "dry-run", false, "Show what would be saved without saving")
}
