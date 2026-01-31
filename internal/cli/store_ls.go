package cli

import (
	"context"

	"github.com/spf13/cobra"
)

var storeLsCmd = &cobra.Command{
	Use:   "ls",
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
			PrintSection("Stores")
			PrintEmptyState("No stores found")
			return nil
		}

		PrintSection("Available Stores")
		rows := make([][]string, 0, len(stores))
		for _, store := range stores {
			rows = append(rows, []string{store.Name, store.Scope})
		}
		PrintTable([]string{"Name", "Scope"}, rows)
		return nil
	},
}
