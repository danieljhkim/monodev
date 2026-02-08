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

Use filter flags to narrow results. Use -v to show all fields.`,
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

		verbose, _ := cmd.Flags().GetBool("verbose")

		PrintSection("Available Stores")
		if verbose {
			printVerboseStoreTable(storeList)
		} else {
			printDefaultStoreTable(storeList)
		}
		return nil
	},
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printDefaultStoreTable(storeList []stores.ScopedStore) {
	rows := make([][]string, 0, len(storeList))
	for _, store := range storeList {
		rows = append(rows, []string{
			store.Meta.Name,
			store.Scope,
			orDash(store.Meta.Status),
			orDash(store.Meta.Type),
			orDash(store.Meta.Source),
			orDash(store.Meta.TaskID),
			orDash(store.Meta.Priority),
		})
	}
	PrintTable([]string{"Name", "Scope", "Status", "Type", "Source", "Task ID", "Priority"}, rows)
}

func printVerboseStoreTable(storeList []stores.ScopedStore) {
	rows := make([][]string, 0, len(storeList))
	for _, store := range storeList {
		rows = append(rows, []string{
			store.Meta.Name,
			store.Scope,
			orDash(store.Meta.Status),
			orDash(store.Meta.Type),
			orDash(store.Meta.Source),
			orDash(store.Meta.Owner),
			orDash(store.Meta.TaskID),
			orDash(store.Meta.ParentTaskID),
			orDash(store.Meta.Priority),
			orDash(store.Meta.Description),
		})
	}
	PrintTable([]string{"Name", "Scope", "Status", "Type", "Source", "Owner", "Task ID", "Parent Task", "Priority", "Description"}, rows)
}

func filterStores(cmd *cobra.Command, storeList []stores.ScopedStore) []stores.ScopedStore {
	filters := []struct {
		flag  string
		match func(stores.ScopedStore, string) bool
	}{
		{"scope", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Scope, v) }},
		{"status", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Meta.Status, v) }},
		{"type", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Meta.Type, v) }},
		{"source", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Meta.Source, v) }},
		{"owner", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Meta.Owner, v) }},
		{"task-id", func(s stores.ScopedStore, v string) bool { return s.Meta.TaskID == v }},
		{"parent-task-id", func(s stores.ScopedStore, v string) bool { return s.Meta.ParentTaskID == v }},
		{"priority", func(s stores.ScopedStore, v string) bool { return strings.EqualFold(s.Meta.Priority, v) }},
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
	storeLsCmd.Flags().BoolP("verbose", "v", false, "Show all fields")
	storeLsCmd.Flags().String("scope", "", "Filter by scope (global, component)")
	storeLsCmd.Flags().String("status", "", "Filter by status")
	storeLsCmd.Flags().String("type", "", "Filter by type")
	storeLsCmd.Flags().String("source", "", "Filter by source")
	storeLsCmd.Flags().String("owner", "", "Filter by owner")
	storeLsCmd.Flags().String("task-id", "", "Filter by task ID")
	storeLsCmd.Flags().String("parent-task-id", "", "Filter by parent task ID")
	storeLsCmd.Flags().String("priority", "", "Filter by priority")
}
