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

		PrintSection("Store Details")

		PrintLabelValue("Name", details.Meta.Name)
		PrintLabelValue("Scope", details.Meta.Scope)
		if details.Meta.Description != "" {
			PrintLabelValue("Description", details.Meta.Description)
		}
		PrintLabelValue("Created", details.Meta.CreatedAt.Format("2006-01-02 15:04:05"))
		PrintLabelValue("Updated", details.Meta.UpdatedAt.Format("2006-01-02 15:04:05"))

		if len(details.TrackedPaths) > 0 {
			PrintSubsection(fmt.Sprintf("\nTracked Paths (%s)", PrintCount(len(details.TrackedPaths), "path", "paths")))
			PrintList(details.TrackedPaths, 1)
		} else {
			PrintSubsection("Tracked Paths")
			PrintEmptyState("No paths tracked")
		}

		return nil
	},
}
