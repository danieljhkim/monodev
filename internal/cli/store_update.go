package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var storeUpdateCmd = &cobra.Command{
	Use:   "update [store-id]",
	Short: "Update store metadata",
	Long:  `Update metadata fields on an existing store. If no store-id is provided, the active store is used.`,
	Args:  cobra.MaximumNArgs(1),
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

		storeScope, _ := cmd.Flags().GetString("scope")

		var storeID string
		if len(args) > 0 {
			storeID = args[0]
		} else {
			// Use active store
			activeID, activeScope, err := eng.GetActiveStoreID(ctx, cwd)
			if err != nil {
				return fmt.Errorf("no store-id provided and %w", err)
			}
			storeID = activeID
			if storeScope == "" {
				storeScope = activeScope
			}
		}

		req := &engine.UpdateStoreRequest{
			CWD:     cwd,
			StoreID: storeID,
			Scope:   storeScope,
		}

		// Only set fields that were explicitly passed
		if cmd.Flags().Changed("description") {
			v, _ := cmd.Flags().GetString("description")
			req.Description = &v
		}
		if cmd.Flags().Changed("owner") {
			v, _ := cmd.Flags().GetString("owner")
			req.Owner = &v
		}
		if cmd.Flags().Changed("task-id") {
			v, _ := cmd.Flags().GetString("task-id")
			req.TaskID = &v
		}

		if err := eng.UpdateStore(ctx, req); err != nil {
			return err
		}

		if jsonOutput {
			result := struct {
				StoreID string `json:"storeId"`
				Updated bool   `json:"updated"`
			}{
				StoreID: storeID,
				Updated: true,
			}
			return outputJSON(result)
		}

		PrintSuccess(fmt.Sprintf("Updated store: %s", storeID))
		return nil
	},
}

func init() {
	storeUpdateCmd.Flags().String("scope", "", "Store scope to disambiguate (global or component)")
	storeUpdateCmd.Flags().String("description", "", "Store description")
	storeUpdateCmd.Flags().String("owner", "", "Store owner")
	storeUpdateCmd.Flags().String("task-id", "", "External task ID")
}
