package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	unapplyForce  bool
	unapplyDryRun bool
)

var unapplyCmd = &cobra.Command{
	Use:   "unapply",
	Short: "Remove active store's overlays from the workspace",
	Long: `Remove overlays applied by the active store from the current workspace.

Paths applied by the stack are not affected - use 'stack unapply' for that.`,
	Args: cobra.NoArgs,
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

		req := &engine.UnapplyRequest{
			CWD:    cwd,
			Force:  unapplyForce,
			DryRun: unapplyDryRun,
		}

		result, err := eng.Unapply(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		if unapplyDryRun {
			PrintSection("Dry Run")
			PrintInfo(fmt.Sprintf("Would remove %s", PrintCount(len(result.Removed), "path", "paths")))
			if len(result.Removed) > 0 {
				PrintSubsection("Paths to remove:")
				PrintList(result.Removed, 1)
			}
			return nil
		}

		PrintSuccess(fmt.Sprintf("Removed %s successfully", PrintCount(len(result.Removed), "path", "paths")))
		return nil
	},
}

func init() {
	unapplyCmd.Flags().BoolVarP(&unapplyForce, "force", "f", false, "Force unapply, bypassing validation")
	unapplyCmd.Flags().BoolVar(&unapplyDryRun, "dry-run", false, "Show what would be removed without removing")
}
