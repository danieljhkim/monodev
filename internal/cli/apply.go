package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	applyMode   string
	applyForce  bool
	applyDryRun bool
)

var applyCmd = &cobra.Command{
	Use:   "apply [store-id]",
	Short: "Apply store overlays to the current workspace",
	Long: `Apply the store stack (plus the active store) to the current working directory.

If [store-id] is provided, it temporarily overrides the active store for this apply.`,
	Args: cobra.MaximumNArgs(1),
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

		req := &engine.ApplyRequest{
			CWD:    cwd,
			Mode:   applyMode,
			Force:  applyForce,
			DryRun: applyDryRun,
		}

		if len(args) > 0 {
			req.StoreID = args[0]
		}

		result, err := eng.Apply(ctx, req)
		if err != nil {
			if result != nil && result.Plan != nil && result.Plan.HasConflicts() {
				PrintError("Conflicts detected:")
				for _, conflict := range result.Plan.Conflicts {
					PrintError(fmt.Sprintf("  %s: %s", conflict.Path, conflict.Reason))
				}
				PrintError("\nUse --force to override conflicts.")
			}
			return err
		}

		if applyDryRun {
			PrintInfo(fmt.Sprintf("Dry run - would apply %d operations", len(result.Plan.Operations)))
			return nil
		}

		PrintSuccess(fmt.Sprintf("Applied %d operations successfully", len(result.Applied)))
		PrintInfo(fmt.Sprintf("Workspace ID: %s", result.WorkspaceID))
		return nil
	},
}

func init() {
	applyCmd.Flags().StringVarP(&applyMode, "mode", "m", "symlink", "Overlay mode (symlink or copy)")
	applyCmd.Flags().BoolVarP(&applyForce, "force", "f", false, "Force apply, overriding conflicts")
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Show what would be applied without applying")
}
