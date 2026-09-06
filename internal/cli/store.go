package cli

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/spf13/cobra"
)

// storeCmd is the parent command for store management.
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Manage stores",
	Long:  `Manage stores for component-scoped development overlays.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	storeCmd.AddCommand(storeCloneCmd)
	storeCmd.AddCommand(storeLsCmd)
	storeCmd.AddCommand(storeRmCmd)
	storeCmd.AddCommand(storeDescribeCmd)
	storeCmd.AddCommand(storeUpdateCmd)
}

var storeCloneCmd = &cobra.Command{
	Use:   "clone <source> <destination>",
	Short: "Clone a saved store",
	Long:  "Clone a saved store into an independent store without activating it or changing the workspace.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		if err := eng.CloneStore(context.Background(), &engine.CloneStoreRequest{
			SourceID:      args[0],
			DestinationID: args[1],
		}); err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(struct {
				SourceID      string `json:"sourceId"`
				DestinationID string `json:"destinationId"`
				Cloned        bool   `json:"cloned"`
			}{
				SourceID:      args[0],
				DestinationID: args[1],
				Cloned:        true,
			})
		}

		PrintSuccess(fmt.Sprintf("Cloned store: %s -> %s", args[0], args[1]))
		return nil
	},
}
