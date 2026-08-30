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
			return printEjectPlan(cmd, plan)
		}

		if !jsonOutput {
			if err := printEjectPlan(cmd, plan); err != nil {
				return err
			}
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
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", warning); err != nil {
				return fmt.Errorf("write eject warning: %w", err)
			}
		}
		if result.RemoveFiles {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Ejected monodev and removed %d overlaid path(s). Stores were kept.\n", len(result.Removed)); err != nil {
				return fmt.Errorf("write eject result: %w", err)
			}
			return nil
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Ejected monodev and kept %d file(s) in place. They are now ordinary workspace files; stores were kept.\n", len(result.Retained)); err != nil {
			return fmt.Errorf("write eject result: %w", err)
		}
		return nil
	},
}

func init() {
	ejectCmd.Flags().BoolVar(&ejectDryRun, "dry-run", false, "Show the eject plan without changing files or state")
	ejectCmd.Flags().BoolVar(&ejectKeepFiles, "keep-files", false, "Keep overlaid files in place (the default)")
	ejectCmd.Flags().BoolVar(&ejectRemoveFiles, "remove-files", false, "Remove every overlaid file")
	ejectCmd.Flags().BoolVar(&ejectYes, "yes", false, "Confirm eject without an interactive prompt")
}

func printEjectPlan(cmd *cobra.Command, result *engine.EjectResult) error {
	mode := "keep files"
	paths := result.Retained
	action := "leave these paths on disk unchanged"
	if result.RemoveFiles {
		mode = "remove files"
		paths = result.Removed
		action = "remove these paths from disk"
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Eject plan (%s):\n", mode); err != nil {
		return fmt.Errorf("write eject plan: %w", err)
	}
	if _, err := fmt.Fprintf(out, "  - %s\n", action); err != nil {
		return fmt.Errorf("write eject plan: %w", err)
	}
	for _, path := range paths {
		if _, err := fmt.Fprintf(out, "  - %s\n", path); err != nil {
			return fmt.Errorf("write eject plan: %w", err)
		}
	}
	if _, err := fmt.Fprintln(out, "  - remove this workspace's ownership ledger"); err != nil {
		return fmt.Errorf("write eject plan: %w", err)
	}
	if _, err := fmt.Fprintln(out, "  - remove monodev's managed .git/info/exclude block"); err != nil {
		return fmt.Errorf("write eject plan: %w", err)
	}
	if _, err := fmt.Fprintln(out, "  - keep all stores; remove them later with `monodev store rm` if wanted"); err != nil {
		return fmt.Errorf("write eject plan: %w", err)
	}
	return nil
}

func confirmEject(cmd *cobra.Command) (bool, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Proceed with eject? [y/N]: "); err != nil {
		return false, fmt.Errorf("write eject confirmation prompt: %w", err)
	}
	response, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read eject confirmation: %w", err)
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}
