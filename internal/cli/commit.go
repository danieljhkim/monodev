package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	commitAll    bool
	commitDryRun bool
)

var commitCmd = &cobra.Command{
	Use:   "commit [path]...",
	Short: "Commit workspace files to the active store",
	Long: `Copy workspace files to the active store.

In symlink mode, only NEW paths (not already managed) are committed.
In copy mode, all specified paths are committed.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Require either paths or --all
		if len(args) == 0 && !commitAll {
			return fmt.Errorf("must specify paths to commit or use --all flag")
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

		req := &engine.CommitRequest{
			CWD:    cwd,
			Paths:  args,
			All:    commitAll,
			DryRun: commitDryRun,
		}

		result, err := eng.Commit(ctx, req)
		if err != nil {
			return err
		}

		if commitDryRun {
			PrintSection("Dry Run")
			PrintInfo(fmt.Sprintf("Would commit %s", PrintCount(len(result.Committed), "path", "paths")))
			if len(result.Committed) > 0 {
				PrintSubsection("Paths to commit:")
				PrintList(result.Committed, 1)
			}
			if len(result.Missing) > 0 {
				fmt.Println()
				PrintWarning(fmt.Sprintf("Would skip %s (not found in workspace):", PrintCount(len(result.Missing), "missing path", "missing paths")))
				PrintList(result.Missing, 1)
			}
			return nil
		}

		PrintSuccess(fmt.Sprintf("Committed %s", PrintCount(len(result.Committed), "path", "paths")))
		if len(result.Skipped) > 0 {
			PrintWarning(fmt.Sprintf("Skipped %s (already managed or not tracked)", PrintCount(len(result.Skipped), "path", "paths")))
		}
		if len(result.Missing) > 0 {
			PrintWarning(fmt.Sprintf("Missing %s (not found in workspace):", PrintCount(len(result.Missing), "path", "paths")))
			PrintList(result.Missing, 1)
		}
		return nil
	},
}

func init() {
	commitCmd.Flags().BoolVar(&commitAll, "all", false, "Commit all tracked paths")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "Show what would be committed without committing")
}
