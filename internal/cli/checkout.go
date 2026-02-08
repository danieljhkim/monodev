package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var checkoutCmd = &cobra.Command{
	Use:   "checkout <store-id>",
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

		// Get flag values
		createNew, _ := cmd.Flags().GetBool("new")
		storeScope, _ := cmd.Flags().GetString("scope")
		storeDesc, _ := cmd.Flags().GetString("description")

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

			if jsonOutput {
				result := struct {
					StoreID     string `json:"storeId"`
					Created     bool   `json:"created"`
					Scope       string `json:"scope,omitempty"`
					Description string `json:"description,omitempty"`
				}{
					StoreID:     storeID,
					Created:     true,
					Scope:       storeScope,
					Description: storeDesc,
				}
				return outputJSON(result)
			}

			PrintSuccess(fmt.Sprintf("Created and activated store: %s", storeID))
			if storeScope != "" {
				PrintLabelValue("Scope", storeScope)
			}
			return nil
		}

		// Select the store as active
		useReq := &engine.UseStoreRequest{
			CWD:     cwd,
			StoreID: storeID,
		}
		if err := eng.UseStore(ctx, useReq); err != nil {
			return err
		}

		if jsonOutput {
			result := struct {
				StoreID string `json:"storeId"`
				Created bool   `json:"created"`
			}{
				StoreID: storeID,
				Created: false,
			}
			return outputJSON(result)
		}

		PrintSuccess(fmt.Sprintf("Active store set to: %s", storeID))
		return nil
	},
}

func init() {
	checkoutCmd.Flags().BoolP("new", "n", false, "Create a new store")
	checkoutCmd.Flags().String("scope", "", "Store scope (global or component; defaults to component if in repo, otherwise global)")
	checkoutCmd.Flags().String("description", "", "Store description")
}
