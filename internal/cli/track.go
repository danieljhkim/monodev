package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var trackCmd = &cobra.Command{
	Use:   "track <path>...",
	Short: "Track paths in the active store",
	Long:  `Add paths to the active store's track file. Paths are resolved relative to the repository root.`,
	Args:  cobra.MinimumNArgs(1),
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

		role, _ := cmd.Flags().GetString("role")
		description, _ := cmd.Flags().GetString("description")
		origin, _ := cmd.Flags().GetString("origin")

		req := &engine.TrackRequest{
			CWD:         cwd,
			Paths:       args,
			Role:        role,
			Description: description,
			Origin:      origin,
		}

		result, err := eng.Track(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			resolvedPaths := make([]string, 0, len(result.ResolvedPaths))
			for _, resolved := range result.ResolvedPaths {
				resolvedPaths = append(resolvedPaths, resolved)
			}
			jsonResult := struct {
				TrackedPaths []string `json:"trackedPaths"`
				MissingPaths []string `json:"missingPaths,omitempty"`
				Count        int      `json:"count"`
			}{
				TrackedPaths: resolvedPaths,
				MissingPaths: result.MissingPaths,
				Count:        len(resolvedPaths),
			}
			return outputJSON(jsonResult)
		}

		// Warn about missing paths
		for _, missing := range result.MissingPaths {
			PrintWarning(fmt.Sprintf("Path not found in workspace: %s", missing))
		}

		trackedCount := len(result.ResolvedPaths)
		if trackedCount > 0 {
			// Show resolved paths when they differ from input
			for _, arg := range args {
				resolved := result.ResolvedPaths[arg]
				if resolved != "" && resolved != arg {
					PrintInfo(fmt.Sprintf("  %s → %s", arg, resolved))
				}
			}
			PrintSuccess(fmt.Sprintf("Tracked %s", PrintCount(trackedCount, "path", "paths")))
		} else {
			PrintWarning("No paths tracked")
		}
		return nil
	},
}

func init() {
	trackCmd.Flags().String("role", "", "Path role (script, docs, style, config, other)")
	trackCmd.Flags().String("description", "", "Description of the tracked path")
	trackCmd.Flags().String("origin", "", "Origin of the tracked path (user, agent, other)")
}
