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
	Short: "Remove previously applied overlays",
	Long:  `Remove all overlays that were previously applied to the current workspace.`,
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

		req := &engine.UnapplyRequest{
			CWD:    cwd,
			Force:  unapplyForce,
			DryRun: unapplyDryRun,
		}

		result, err := eng.Unapply(ctx, req)
		if err != nil {
			return err
		}

		if unapplyDryRun {
			PrintInfo(fmt.Sprintf("Dry run - would remove %d paths", len(result.Removed)))
			return nil
		}

		PrintSuccess(fmt.Sprintf("Removed %d paths successfully", len(result.Removed)))
		return nil
	},
}

func init() {
	unapplyCmd.Flags().BoolVarP(&unapplyForce, "force", "f", false, "Force unapply, bypassing validation")
	unapplyCmd.Flags().BoolVar(&unapplyDryRun, "dry-run", false, "Show what would be removed without removing")
}
