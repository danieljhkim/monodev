package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	applyForce  bool
	applyDryRun bool
)

var applyCmd = &cobra.Command{
	Use:   "apply [store-id...]",
	Short: "Apply store overlays to the current workspace",
	Long: `Apply one or more stores to the current working directory.

With no arguments, applies the active store. With store IDs, applies those
stores in argument order. Later stores take precedence on path conflicts.`,
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

		req := &engine.ApplyRequest{
			CWD:      cwd,
			Mode:     "copy",
			Force:    applyForce,
			DryRun:   applyDryRun,
			StoreIDs: append([]string{}, args...),
		}

		result, err := eng.Apply(ctx, req)
		if err != nil {
			if result != nil && result.Plan != nil && result.Plan.HasConflicts() {
				if jsonOutput {
					return outputJSON(result)
				}
				PrintSection("Conflicts Detected")
				for _, conflict := range result.Plan.Conflicts {
					PrintError(fmt.Sprintf("%s: %s", conflict.Path, conflict.Reason))
				}
				fmt.Println()
				PrintWarning("Use --force to override conflicts.")
			}
			return err
		}

		if jsonOutput {
			return outputJSON(result)
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
					case "create_symlink": // Deprecated, kept for backward compatibility
						opType = "symlink"
					case "copy":
						opType = "copy"
					case "remove":
						opType = "remove"
					default:
						opType = op.Type
					}
					if op.Store != "" {
						ops = append(ops, fmt.Sprintf("%s: %s (from %s)", opType, op.RelPath, op.Store))
					} else {
						ops = append(ops, fmt.Sprintf("%s: %s", opType, op.RelPath))
					}
				}
				PrintList(ops, 1)
			}
			return nil
		}

		if result.Plan != nil && len(result.Plan.Warnings) > 0 {
			for _, w := range result.Plan.Warnings {
				PrintWarning(w)
			}
		}

		PrintSuccess(fmt.Sprintf("Applied %s successfully", PrintCount(len(result.Applied), "operation", "operations")))
		PrintLabelValue("Workspace ID", result.WorkspaceID)
		return nil
	},
}

func init() {
	applyCmd.Flags().BoolVarP(&applyForce, "force", "f", false, "Force apply, overriding conflicts")
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Show what would be applied without applying")
}
