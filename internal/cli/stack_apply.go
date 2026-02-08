package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

// stackApplyCmd applies all stores in the stack.
var stackApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply the stack (multiple stores) in dependency order ",
	Long: `Apply all stores in the stack to the current workspace.
Stores are applied in order, with later stores taking precedence on path conflicts.
The active store is not affected - use 'monodev apply' separately for that.`,
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

		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		applyMode := "copy" // cmd.Flags().GetString("mode")

		req := &engine.StackApplyRequest{
			CWD:    cwd,
			Mode:   applyMode,
			Force:  force,
			DryRun: dryRun,
		}

		result, err := eng.StackApply(ctx, req)
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

		if jsonOutput {
			return outputJSON(result)
		}

		if dryRun {
			PrintSection("Stack Apply (Dry Run)")
			PrintInfo(fmt.Sprintf("Would apply %s", PrintCount(len(result.Plan.Operations), "operation", "operations")))
			if len(result.Plan.Operations) > 0 {
				PrintSubsection("Operations:")
				ops := make([]string, 0, len(result.Plan.Operations))
				for _, op := range result.Plan.Operations {
					ops = append(ops, fmt.Sprintf("%s: %s (from %s)", op.Type, op.RelPath, op.Store))
				}
				PrintList(ops, 1)
			}
			return nil
		}

		// Show warnings for missing tracked paths
		if result.Plan != nil && len(result.Plan.Warnings) > 0 {
			for _, w := range result.Plan.Warnings {
				PrintWarning(w)
			}
		}

		PrintSuccess(fmt.Sprintf("Applied %s from stack successfully", PrintCount(len(result.Applied), "operation", "operations")))
		PrintLabelValue("Workspace ID", result.WorkspaceID)
		return nil
	},
}

// stackUnapplyCmd removes stack-applied overlays.
var stackUnapplyCmd = &cobra.Command{
	Use:   "unapply",
	Short: "Unapply the stack (reverse dependency order), best-effort",
	Long: `Remove overlays applied by the stack.
Paths applied by the active store are not affected - use 'monodev unapply' for that.`,
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

		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		req := &engine.StackUnapplyRequest{
			CWD:    cwd,
			Force:  force,
			DryRun: dryRun,
		}

		result, err := eng.StackUnapply(ctx, req)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(result)
		}

		if dryRun {
			PrintSection("Stack Unapply (Dry Run)")
			PrintInfo(fmt.Sprintf("Would remove %s", PrintCount(len(result.Removed), "path", "paths")))
			if len(result.Removed) > 0 {
				PrintSubsection("Paths to remove:")
				PrintList(result.Removed, 1)
			}
			return nil
		}

		PrintSuccess(fmt.Sprintf("Removed %s from stack successfully", PrintCount(len(result.Removed), "path", "paths")))
		return nil
	},
}
