package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:   "describe <store-id>",
	Short: "Show store details",
	Long:  `Display detailed information about a store.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		storeID := args[0]

		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		details, err := eng.DescribeStore(ctx, storeID)
		if err != nil {
			return err
		}

		fmt.Printf("Store: %s\n", details.Meta.Name)
		fmt.Printf("Scope: %s\n", details.Meta.Scope)
		if details.Meta.Description != "" {
			fmt.Printf("Description: %s\n", details.Meta.Description)
		}
		fmt.Printf("Created: %s\n", details.Meta.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("Updated: %s\n", details.Meta.UpdatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("\nTracked paths (%d):\n", len(details.TrackedPaths))
		for _, path := range details.TrackedPaths {
			fmt.Printf("  %s\n", path)
		}
		return nil
	},
}
