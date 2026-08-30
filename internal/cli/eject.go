package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var (
	ejectDryRun      bool
	ejectKeepFiles   bool
	ejectRemoveFiles bool
	ejectYes         bool
)

var ejectCmd = &cobra.Command{
	Use:   "eject",
	Short: "Detach this workspace from monodev",
	Long: `Detach the current workspace from monodev while keeping all stores.

By default, eject keeps every overlaid file exactly as it is, removes this
workspace's ownership ledger, and removes monodev's managed .git/info/exclude
block so the retained files become ordinary untracked files. Use --remove-files
to delete every overlaid path instead. Both modes leave stores in place for a
later explicit store rm.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if ejectKeepFiles && ejectRemoveFiles {
			return fmt.Errorf("--keep-files and --remove-files cannot be combined")
		}
		if jsonOutput && !ejectDryRun && !ejectYes {
			return fmt.Errorf("--json requires --yes for eject; confirmation is required")
		}

		eng, err := newEngine()
		if err != nil {
			return err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		req := &engine.EjectRequest{
			CWD:         cwd,
			RemoveFiles: ejectRemoveFiles,
			DryRun:      true,
		}
		plan, err := eng.Eject(context.Background(), req)
		if err != nil {
			return err
		}

		if ejectDryRun {
			if jsonOutput {
				return outputJSON(plan)
			}
			printEjectPlan(cmd, plan)
			return nil
		}

		if !jsonOutput {
			printEjectPlan(cmd, plan)
		}
		if !ejectYes {
			confirmed, confirmErr := confirmEject(cmd)
			if confirmErr != nil {
				return confirmErr
			}
			if !confirmed {
				return fmt.Errorf("eject cancelled by user")
			}
		}

		req.DryRun = false
		result, err := eng.Eject(context.Background(), req)
		if err != nil {
			return err
		}
		if jsonOutput {
			return outputJSON(result)
		}

		for _, warning := range result.Warnings {
			fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", warning)
		}
		if result.RemoveFiles {
			fmt.Fprintf(cmd.OutOrStdout(), "Ejected monodev and removed %d overlaid path(s). Stores were kept.\n", len(result.Removed))
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Ejected monodev and kept %d file(s) in place. They are now ordinary workspace files; stores were kept.\n", len(result.Retained))
		return nil
	},
}

func init() {
	ejectCmd.Flags().BoolVar(&ejectDryRun, "dry-run", false, "Show the eject plan without changing files or state")
	ejectCmd.Flags().BoolVar(&ejectKeepFiles, "keep-files", false, "Keep overlaid files in place (the default)")
	ejectCmd.Flags().BoolVar(&ejectRemoveFiles, "remove-files", false, "Remove every overlaid file")
	ejectCmd.Flags().BoolVar(&ejectYes, "yes", false, "Confirm eject without an interactive prompt")
}

func printEjectPlan(cmd *cobra.Command, result *engine.EjectResult) {
	mode := "keep files"
	paths := result.Retained
	action := "leave these paths on disk unchanged"
	if result.RemoveFiles {
		mode = "remove files"
		paths = result.Removed
		action = "remove these paths from disk"
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Eject plan (%s):\n", mode)
	fmt.Fprintf(out, "  - %s\n", action)
	for _, path := range paths {
		fmt.Fprintf(out, "  - %s\n", path)
	}
	fmt.Fprintln(out, "  - remove this workspace's ownership ledger")
	fmt.Fprintln(out, "  - remove monodev's managed .git/info/exclude block")
	fmt.Fprintln(out, "  - keep all stores; remove them later with `monodev store rm` if wanted")
}

func confirmEject(cmd *cobra.Command) (bool, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Proceed with eject? [y/N]: ")
	response, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read eject confirmation: %w", err)
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}
