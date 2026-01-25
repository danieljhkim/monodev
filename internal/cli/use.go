package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	createNew  bool
	storeScope string
	storeDesc  string
)

var useCmd = &cobra.Command{
	Use:   "use <store-id>",
	Short: "Select a store as active",
	Long: `Select an existing store as the active store for the current repository.

Use -n to create a new store if it doesn't exist.`,
	Args: cobra.ExactArgs(1),
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

		// If -n flag is set, create the store (which also sets it as active)
		if createNew {
			createReq := &engine.CreateStoreRequest{
				CWD:         cwd,
				StoreID:     storeID,
				Name:        storeID,
				Scope:       storeScope,
				Description: storeDesc,
			}
			if err := eng.CreateStore(ctx, createReq); err != nil {
				return fmt.Errorf("failed to create store: %w", err)
			}
			PrintSuccess(fmt.Sprintf("Created and activated store: %s", storeID))
			return nil
		}

		// Select the store as active
		useReq := &engine.UseStoreRequest{
			CWD:     cwd,
			StoreID: storeID,
		}
		if err := eng.UseStore(ctx, useReq); err != nil {
			return fmt.Errorf("failed to use store: %w", err)
		}

		PrintSuccess(fmt.Sprintf("Active store set to: %s", storeID))
		return nil
	},
}

func init() {
	useCmd.Flags().BoolVarP(&createNew, "new", "n", false, "Create a new store")
	useCmd.Flags().StringVar(&storeScope, "scope", "component", "Store scope (global, profile, component)")
	useCmd.Flags().StringVar(&storeDesc, "description", "", "Store description")
}
