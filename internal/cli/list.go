package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stores",
	Long:  `Display all available stores.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		stores, err := eng.ListStores(ctx)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(stores)
		}

		if len(stores) == 0 {
			PrintInfo("No stores found")
			return nil
		}

		PrintInfo("Available stores:")
		for _, store := range stores {
			PrintInfo(fmt.Sprintf("  %s (%s)", store.Name, store.Scope))
		}
		return nil
	},
}
