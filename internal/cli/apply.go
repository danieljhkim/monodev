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
	Short: "Apply a single store's overlays to the current workspace",
	Long: `Apply the active store (or specified store) to the current working directory.

If [store-id] is provided, it overrides the active store for this apply.
This command applies only a single store - use 'stack apply' to apply the stack.`,
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
				PrintSection("Conflicts Detected")
				for _, conflict := range result.Plan.Conflicts {
					PrintError(fmt.Sprintf("%s: %s", conflict.Path, conflict.Reason))
				}
				fmt.Println()
				PrintWarning("Use --force to override conflicts.")
			}
			return err
		}

		if applyDryRun {
			PrintSection("Dry Run")
			PrintInfo(fmt.Sprintf("Would apply %s", PrintCount(len(result.Plan.Operations), "operation", "operations")))
			if len(result.Plan.Operations) > 0 {
				PrintSubsection("Operations:")
				ops := make([]string, 0, len(result.Plan.Operations))
				for _, op := range result.Plan.Operations {
					var opType string
					switch op.Type {
					case "create_symlink":
						opType = "symlink"
					case "copy":
						opType = "copy"
					case "remove":
						opType = "remove"
					default:
						opType = op.Type
					}
					ops = append(ops, fmt.Sprintf("%s: %s", opType, op.RelPath))
				}
				PrintList(ops, 1)
			}
			return nil
		}

		PrintSuccess(fmt.Sprintf("Applied %s successfully", PrintCount(len(result.Applied), "operation", "operations")))
		PrintLabelValue("Workspace ID", result.WorkspaceID)
		return nil
	},
}

func init() {
	applyCmd.Flags().StringVarP(&applyMode, "mode", "m", "symlink", "Overlay mode (symlink or copy)")
	applyCmd.Flags().BoolVarP(&applyForce, "force", "f", false, "Force apply, overriding conflicts")
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Show what would be applied without applying")
}
