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

		if jsonOutput {
			return outputJSON(details)
		}

		PrintInfo(fmt.Sprintf("Store: %s", details.Meta.Name))
		PrintInfo(fmt.Sprintf("Scope: %s", details.Meta.Scope))
		if details.Meta.Description != "" {
			PrintInfo(fmt.Sprintf("Description: %s", details.Meta.Description))
		}
		PrintInfo(fmt.Sprintf("Created: %s", details.Meta.CreatedAt.Format("2006-01-02 15:04:05")))
		PrintInfo(fmt.Sprintf("Updated: %s", details.Meta.UpdatedAt.Format("2006-01-02 15:04:05")))
		PrintInfo(fmt.Sprintf("\nTracked paths (%d):", len(details.TrackedPaths)))
		for _, path := range details.TrackedPaths {
			PrintInfo(fmt.Sprintf("  %s", path))
		}
		return nil
	},
}
