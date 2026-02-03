package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

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
	Long:  `Add a store to the stack. The stores in the stack are applied in order, with later stores taking precedence on path conflicts.`,
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
		PrintInfo(fmt.Sprintf("Store '%s' will be applied in the order of the stack.", storeID))
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
