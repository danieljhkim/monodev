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
- Delete store overlay content

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

		result, err := eng.Untrack(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			jsonResult := struct {
				UntrackedPaths []string `json:"untrackedPaths"`
				NotFoundPaths  []string `json:"notFoundPaths,omitempty"`
				Count          int      `json:"count"`
			}{
				UntrackedPaths: result.RemovedPaths,
				NotFoundPaths:  result.NotFoundPaths,
				Count:          len(result.RemovedPaths),
			}
			return outputJSON(jsonResult)
		}

		// Warn about paths not found in track file
		for _, p := range result.NotFoundPaths {
			PrintWarning(fmt.Sprintf("Path not found in workspace: %s", p))
		}

		if len(result.RemovedPaths) > 0 {
			PrintSuccess(fmt.Sprintf("Untracked %s", PrintCount(len(result.RemovedPaths), "path", "paths")))
			PrintWarning("Note: Store overlay content not deleted.")
		} else {
			PrintWarning("No paths untracked")
		}
		return nil
	},
}
