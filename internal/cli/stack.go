package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

// stackCmd is the parent command for stack management.
var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage the store stack",
	Long: `Manage the persistent store stack for the current repository.

The store stack determines which stores are applied when running 'monodev stack apply'.
Stores are applied in order, with later stores taking precedence on path conflicts.`,
}

// stackLsCmd lists stores in the stack.
var stackLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List stores in the stack",
	Long:  `List all stores in the stack, in order. Later stores take precedence.`,
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

		req := &engine.StackListRequest{
			CWD: cwd,
		}

		result, err := eng.StackList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list stack: %w", err)
		}

		if jsonOutput {
			output := struct {
				Stack       []string `json:"stack"`
				ActiveStore string   `json:"activeStore"`
			}{
				Stack:       result.Stack,
				ActiveStore: result.ActiveStore,
			}
			return outputJSON(output)
		}

		PrintSection("Store Stack")

		if len(result.Stack) == 0 {
			PrintSubsection("Stack (in order of precedence):")
			PrintEmptyState("Stack is empty")
		} else {
			PrintSubsection("Stack (in order of precedence):")
			PrintNumberedList(result.Stack, 1)
		}

		return nil
	},
}

// stackAddCmd adds a store to the stack.
var stackAddCmd = &cobra.Command{
	Use:   "add <store-id>",
	Short: "Add a store to the stack",
	Long:  `Add a store to the stack. The store will be applied before the active store.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		storeID := args[0]

		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		req := &engine.StackAddRequest{
			CWD:     cwd,
			StoreID: storeID,
		}

		if err := eng.StackAdd(ctx, req); err != nil {
			return fmt.Errorf("failed to add store to stack: %w", err)
		}

		PrintSuccess(fmt.Sprintf("Added store to stack: %s", storeID))
		PrintInfo(fmt.Sprintf("Store '%s' will be applied before the active store.", storeID))
		return nil
	},
}

// stackPopCmd removes a store from the stack.
var stackPopCmd = &cobra.Command{
	Use:   "pop [<store-id>]",
	Short: "Remove a store from the stack",
	Long: `Remove a store from the stack.

If no store-id is provided, removes the last store (LIFO).
If a store-id is provided, removes that specific store from the stack.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var storeID string
		if len(args) > 0 {
			storeID = args[0]
		}

		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		req := &engine.StackPopRequest{
			CWD:     cwd,
			StoreID: storeID,
		}

		result, err := eng.StackPop(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to remove store from stack: %w", err)
		}

		PrintSuccess(fmt.Sprintf("Removed store from stack: %s", result.Removed))
		return nil
	},
}

// stackClearCmd clears the entire stack.
var stackClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the entire stack",
	Long:  `Remove all stores from the stack. The active store is not affected.`,
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

		req := &engine.StackClearRequest{
			CWD: cwd,
		}

		if err := eng.StackClear(ctx, req); err != nil {
			return fmt.Errorf("failed to clear stack: %w", err)
		}

		PrintSuccess("Stack cleared")
		return nil
	},
}

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

		req := &engine.StackApplyRequest{
			CWD:    cwd,
			Mode:   "symlink", // Only symlink mode supported for now
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

func init() {
	stackCmd.AddCommand(stackLsCmd)
	stackCmd.AddCommand(stackAddCmd)
	stackCmd.AddCommand(stackPopCmd)
	stackCmd.AddCommand(stackClearCmd)
	stackCmd.AddCommand(stackApplyCmd)
	stackCmd.AddCommand(stackUnapplyCmd)

	// Flags for stack apply
	// Note: Only symlink mode is supported for stack operations for now
	stackApplyCmd.Flags().BoolP("force", "f", false, "Force apply, overwriting conflicts")
	stackApplyCmd.Flags().Bool("dry-run", false, "Show what would be applied without making changes")

	// Flags for stack unapply
	stackUnapplyCmd.Flags().BoolP("force", "f", false, "Force removal even if validation fails")
	stackUnapplyCmd.Flags().Bool("dry-run", false, "Show what would be removed without making changes")
}
