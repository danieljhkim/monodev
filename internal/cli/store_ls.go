package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/stores"
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

		storeList, err := eng.ListStores(ctx)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(storeList)
		}

		if len(storeList) == 0 {
			PrintSection("Stores")
			PrintEmptyState("No stores found")
			return nil
		}

		PrintSection("Available Stores")
		printStoreTable(storeList)
		return nil
	},
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printStoreTable(storeList []stores.ScopedStore) {
	rows := make([][]string, 0, len(storeList))
	for _, store := range storeList {
		rows = append(rows, []string{
			store.Meta.Name,
			orDash(store.Meta.Description),
		})
	}
	PrintTable([]string{"Name", "Description"}, rows)
}
