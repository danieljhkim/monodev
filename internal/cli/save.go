package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/stores"
)

var saveDryRun bool

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "Track new files under tracked directories and commit everything",
	Long: `save is the everyday verb for keeping a store current.

It detects files created under tracked directories since the store was last
committed, tracks them in the store's track.json, then commits everything
tracked to the active store. Run it after a working session instead of
chaining 'monodev track' and 'monodev commit --all' yourself.`,
	Args: cobra.NoArgs,
	RunE: runSave,
}

func init() {
	saveCmd.Flags().BoolVar(&saveDryRun, "dry-run", false, "Preview what save would track and commit without making changes")
}

func runSave(cmd *cobra.Command, args []string) error {
	eng, err := newEngine()
	if err != nil {
		return err
	}

	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	discovered, err := eng.DiscoverNewTracked(ctx, &engine.DiscoverNewTrackedRequest{CWD: cwd})
	if err != nil {
		return err
	}

	var newlyTracked []string
	if len(discovered.NewPaths) > 0 {
		if saveDryRun {
			newlyTracked = discovered.NewPaths
		} else {
			trackResult, err := eng.Track(ctx, &engine.TrackRequest{
				CWD:    cwd,
				Paths:  discovered.NewPaths,
				Origin: stores.OriginUser,
			})
			if err != nil {
				return err
			}
			for _, userPath := range discovered.NewPaths {
				if resolved, ok := trackResult.ResolvedPaths[userPath]; ok {
					newlyTracked = append(newlyTracked, resolved)
				}
			}
		}
	}

	commitResult, err := eng.Commit(ctx, &engine.CommitRequest{
		CWD:    cwd,
		All:    true,
		DryRun: saveDryRun,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		result := struct {
			NewlyTracked []string `json:"newlyTracked"`
			Committed    []string `json:"committed"`
			Missing      []string `json:"missing,omitempty"`
			Removed      []string `json:"removed,omitempty"`
			DryRun       bool     `json:"dryRun"`
		}{
			NewlyTracked: newlyTracked,
			Committed:    commitResult.Committed,
			Missing:      commitResult.Missing,
			Removed:      commitResult.Removed,
			DryRun:       saveDryRun,
		}
		return outputJSON(result)
	}

	printSaveResult(newlyTracked, commitResult, saveDryRun)
	return nil
}

func printSaveResult(newlyTracked []string, commitResult *engine.CommitResult, dryRun bool) {
	if dryRun {
		PrintSection("Dry Run")
	}

	if len(newlyTracked) > 0 {
		verb := "Newly tracked"
		if dryRun {
			verb = "Would newly track"
		}
		PrintInfo(fmt.Sprintf("%s %s:", verb, PrintCount(len(newlyTracked), "path", "paths")))
		PrintList(newlyTracked, 1)
	}

	commitVerb := "Committed"
	if dryRun {
		commitVerb = "Would commit"
	}
	PrintSuccess(fmt.Sprintf("%s %s", commitVerb, PrintCount(len(commitResult.Committed), "path", "paths")))

	if len(commitResult.Missing) > 0 {
		PrintWarning(fmt.Sprintf("Missing %s (not found in workspace):", PrintCount(len(commitResult.Missing), "path", "paths")))
		PrintList(commitResult.Missing, 1)
	}
	if len(commitResult.Removed) > 0 {
		removeVerb := "Removed"
		if dryRun {
			removeVerb = "Would remove"
		}
		PrintInfo(fmt.Sprintf("%s %s from store (no longer tracked):", removeVerb, PrintCount(len(commitResult.Removed), "path", "paths")))
		PrintList(commitResult.Removed, 1)
	}
}
