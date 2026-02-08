package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var storeDescribeCmd = &cobra.Command{
	Use:   "describe [store-id]",
	Short: "Show store details",
	Long:  `Display detailed information about a store. If no store-id is provided, the active store is used.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()

		var storeID string
		if len(args) > 0 {
			storeID = args[0]
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			activeID, _, err := eng.GetActiveStoreID(ctx, cwd)
			if err != nil {
				return fmt.Errorf("no store-id provided and %w", err)
			}
			storeID = activeID
		}

		detailsList, err := eng.DescribeStore(ctx, storeID)
		if err != nil {
			return err
		}

		if jsonOutput {
			return outputJSON(detailsList)
		}

		for i, details := range detailsList {
			if len(detailsList) > 1 {
				PrintSection(fmt.Sprintf("Store Details (%s)", details.Scope))
			} else {
				PrintSection("Store Details")
			}

			PrintLabelValue("Name", details.Meta.Name)
			PrintLabelValue("Scope", details.Scope)
			if details.Meta.Description != "" {
				PrintLabelValue("Description", details.Meta.Description)
			}
			PrintLabelValue("Created", details.Meta.CreatedAt.Format("2006-01-02 15:04:05"))
			PrintLabelValue("Updated", details.Meta.UpdatedAt.Format("2006-01-02 15:04:05"))

			if details.Meta.Source != "" {
				PrintLabelValue("Source", details.Meta.Source)
			}
			if details.Meta.Type != "" {
				PrintLabelValue("Type", details.Meta.Type)
			}
			if details.Meta.Owner != "" {
				PrintLabelValue("Owner", details.Meta.Owner)
			}
			if details.Meta.TaskID != "" {
				PrintLabelValue("Task ID", details.Meta.TaskID)
			}
			if details.Meta.ParentTaskID != "" {
				PrintLabelValue("Parent Task ID", details.Meta.ParentTaskID)
			}
			if details.Meta.Priority != "" {
				PrintLabelValue("Priority", details.Meta.Priority)
			}
			if details.Meta.Status != "" {
				PrintLabelValue("Status", details.Meta.Status)
			}

			if len(details.TrackedPaths) > 0 {
				PrintSubsection(fmt.Sprintf("\nTracked Paths (%s)", PrintCount(len(details.TrackedPaths), "path", "paths")))
				pathStrs := make([]string, len(details.TrackedPaths))
				for i, tp := range details.TrackedPaths {
					if tp.Role != "" {
						pathStrs[i] = fmt.Sprintf("%s [%s]", tp.Path, tp.Role)
					} else {
						pathStrs[i] = tp.Path
					}
				}
				PrintList(pathStrs, 1)
			} else {
				PrintSubsection("Tracked Paths")
				PrintEmptyState("No paths tracked")
			}

			if i < len(detailsList)-1 {
				fmt.Println()
			}
		}

		return nil
	},
}
