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
	Use:   "unapply [store-id...]",
	Short: "Remove applied overlays from the workspace",
	Long: `Remove overlays applied by one or more stores from the current workspace.

With no arguments, removes paths owned by the active store. With store IDs,
removes only paths owned by those stores.`,
	Args: cobra.ArbitraryArgs,
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
			CWD:      cwd,
			Force:    unapplyForce,
			DryRun:   unapplyDryRun,
			StoreIDs: append([]string{}, args...),
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

		for _, warning := range result.Warnings {
			PrintWarning(warning)
		}

		PrintSuccess(fmt.Sprintf("Removed %s successfully", PrintCount(len(result.Removed), "path", "paths")))
		return nil
	},
}

func init() {
	unapplyCmd.Flags().BoolVarP(&unapplyForce, "force", "f", false, "Force unapply, bypassing validation")
	unapplyCmd.Flags().BoolVar(&unapplyDryRun, "dry-run", false, "Show what would be removed without removing")
}
