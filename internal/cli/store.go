package cli

import (
	"github.com/spf13/cobra"
)

// storeCmd is the parent command for store management.
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Manage stores",
	Long:  `Manage stores for component-scoped development overlays.`,
}

func init() {
	storeCmd.AddCommand(storeLsCmd)
	storeCmd.AddCommand(storeRmCmd)
	storeCmd.AddCommand(storeDescribeCmd)
	storeCmd.AddCommand(storeUpdateCmd)
}
