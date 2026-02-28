package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/stores"
)

var storeLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all stores",
	Long: `Display all available stores.

Use filter flags to narrow results.`,
	Args: cobra.NoArgs,
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

		// Apply filters
		storeList = filterStores(cmd, storeList)

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
			store.Scope,
			orDash(store.Meta.Owner),
			orDash(store.Meta.Description),
		})
	}
	PrintTable([]string{"Name", "Scope", "Owner", "Description"}, rows)
}

func filterStores(cmd *cobra.Command, storeList []stores.ScopedStore) []stores.ScopedStore {
	filters := []struct {
		flag  string
		match func(stores.ScopedStore, string) bool
	}{
		{"scope", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Scope, v) }},
		{"owner", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Meta.Owner, v) }},
	}

	for _, f := range filters {
		val, _ := cmd.Flags().GetString(f.flag)
		if val == "" {
			continue
		}
		filtered := make([]stores.ScopedStore, 0, len(storeList))
		for _, s := range storeList {
			if f.match(s, val) {
				filtered = append(filtered, s)
			}
		}
		storeList = filtered
	}
	return storeList
}

func init() {
	storeLsCmd.Flags().String("scope", "", "Filter by scope (global, component)")
	storeLsCmd.Flags().String("owner", "", "Filter by owner")
}
