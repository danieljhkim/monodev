package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	storeRmForce  bool
	storeRmDryRun bool
)

var storeRmCmd = &cobra.Command{
	Use:   "rm <store-id>",
	Short: "Delete a store and all its contents",
	Long: `Delete a store permanently, including all overlay content.

This command will check if the store is in use by any workspace before deletion.
If the store is in use, you'll be prompted to confirm deletion unless --force is used.

Deleting a store will:
  - Remove all overlay content permanently
  - Clear references from all workspace states
  - NOT remove applied files from workspaces (use 'monodev unapply' first)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		storeID := args[0]

		req := &engine.DeleteStoreRequest{
			StoreID: storeID,
			Force:   storeRmForce,
			DryRun:  storeRmDryRun,
		}

		result, err := eng.DeleteStore(ctx, req)

		// Handle JSON output
		if jsonOutput {
			return outputDeleteJSON(result, err)
		}

		// If error and store is in use without force, show usage and prompt
		if err != nil && result != nil && len(result.AffectedWorkspaces) > 0 && !storeRmForce && !storeRmDryRun {
			PrintSection("Delete Store")
			PrintWarning(fmt.Sprintf("Store '%s' is in use by %d workspace(s):", storeID, len(result.AffectedWorkspaces)))
			fmt.Println()

			// Display affected workspaces
			for _, usage := range result.AffectedWorkspaces {
				fmt.Printf("  %s\n", usage.WorkspacePath)
				details := []string{}
				if usage.IsActive {
					details = append(details, "Active store")
				}
				if usage.InStack {
					details = append(details, "In stack")
				}
				if usage.AppliedPathCount > 0 {
					details = append(details, fmt.Sprintf("%d applied paths", usage.AppliedPathCount))
				}
				for _, detail := range details {
					PrintInfo(fmt.Sprintf("    - %s", detail))
				}
				fmt.Println()
			}

			// Show consequences
			PrintWarning("Deleting will:")
			PrintList([]string{
				"Remove all overlay content permanently",
				"Clear references from workspaces",
				"NOT remove applied files (use 'monodev unapply' first)",
			}, 1)
			fmt.Println()

			// Prompt for confirmation
			if !promptConfirm("Proceed?") {
				return fmt.Errorf("deletion cancelled by user")
			}

			// Retry with force
			req.Force = true
			result, err = eng.DeleteStore(ctx, req)
		}

		if err != nil {
			return err
		}

		// Handle dry-run output
		if storeRmDryRun {
			PrintSection("Dry Run: Delete Store")
			PrintInfo(fmt.Sprintf("Store: %s", result.StoreID))
			fmt.Println()

			if len(result.AffectedWorkspaces) > 0 {
				PrintWarning(fmt.Sprintf("Store is in use by %d workspace(s):", len(result.AffectedWorkspaces)))
				for _, usage := range result.AffectedWorkspaces {
					details := []string{}
					if usage.IsActive {
						details = append(details, "active store")
					}
					if usage.InStack {
						details = append(details, "in stack")
					}
					if usage.AppliedPathCount > 0 {
						details = append(details, fmt.Sprintf("%d applied paths", usage.AppliedPathCount))
					}
					PrintInfo(fmt.Sprintf("  %s (%s)", usage.WorkspacePath, strings.Join(details, ", ")))
				}
				fmt.Println()
			}

			PrintWarning("Run without --dry-run to delete")
			return nil
		}

		// Success output
		PrintSection("Delete Store")
		PrintSuccess(fmt.Sprintf("Deleted store: %s", result.StoreID))

		if len(result.AffectedWorkspaces) > 0 {
			fmt.Println()
			PrintInfo(fmt.Sprintf("Cleaned references from %d workspace(s)", len(result.AffectedWorkspaces)))
			for _, usage := range result.AffectedWorkspaces {
				PrintList([]string{usage.WorkspacePath}, 1)
			}
		}

		return nil
	},
}

func init() {
	storeRmCmd.Flags().BoolVarP(&storeRmForce, "force", "f", false, "Force deletion without confirmation")
	storeRmCmd.Flags().BoolVar(&storeRmDryRun, "dry-run", false, "Show what would be deleted without deleting")
}

// promptConfirm prompts the user for a yes/no confirmation.
func promptConfirm(prompt string) bool {
	fmt.Printf("%s (y/N): ", prompt)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// outputDeleteJSON outputs the delete result in JSON format.
func outputDeleteJSON(result *engine.DeleteStoreResult, err error) error {
	output := map[string]any{
		"success": err == nil,
	}

	if result != nil {
		output["storeId"] = result.StoreID
		output["deleted"] = result.Deleted
		output["dryRun"] = result.DryRun

		if len(result.AffectedWorkspaces) > 0 {
			workspaces := make([]map[string]any, len(result.AffectedWorkspaces))
			for i, usage := range result.AffectedWorkspaces {
				workspaces[i] = map[string]any{
					"workspaceId":      usage.WorkspaceID,
					"workspacePath":    usage.WorkspacePath,
					"isActive":         usage.IsActive,
					"inStack":          usage.InStack,
					"appliedPathCount": usage.AppliedPathCount,
				}
			}
			output["affectedWorkspaces"] = workspaces
		}
	}

	if err != nil {
		output["error"] = err.Error()
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
