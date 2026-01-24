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

		if len(stores) == 0 {
			fmt.Println("No stores found")
			return nil
		}

		fmt.Printf("Available stores:\n")
		for _, store := range stores {
			fmt.Printf("  %s (%s)\n", store.Name, store.Scope)
		}
		return nil
	},
}
