package cli

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/sync"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [store-id...]",
	Short: "Push stores to remote persistence repository",
	Long: `Push stores to the remote persistence repository.

Stores are pushed to a separate Git orphan branch at monodev/persist
by default. This allows sharing stores across machines and teams.

If no store IDs are specified, pushes all local stores.

Examples:
  # Push all local stores
  monodev push

  # Push a single store
  monodev push my-store

  # Push multiple stores
  monodev push store1 store2

  # Push with workspace references
  monodev push my-store --with-workspace

  # Dry run to see what would be pushed
  monodev push my-store --dry-run

  # Force push (overwrite remote)
  monodev push my-store --force`,
	Args: cobra.ArbitraryArgs,
	RunE: runPush,
}

var (
	pushWithWorkspace bool
	pushRemote        string
	pushDryRun        bool
	pushForce         bool
)

func init() {
	pushCmd.Flags().BoolVar(&pushWithWorkspace, "with-workspace", false, "Push workspace references along with stores")
	pushCmd.Flags().StringVar(&pushRemote, "remote", "", "Git remote to push to (defaults to configured remote)")
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Show what would be pushed without actually pushing")
	pushCmd.Flags().BoolVar(&pushForce, "force", false, "Force push (overwrite remote changes)")
}

func runPush(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get the repository root
	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Create syncer
	syncer, err := newSyncer()
	if err != nil {
		return fmt.Errorf("failed to create syncer: %w", err)
	}

	// Build request
	req := &sync.PushRequest{
		RepoRoot:      repoRoot,
		StoreIDs:      args,
		WithWorkspace: pushWithWorkspace,
		Remote:        pushRemote,
		DryRun:        pushDryRun,
		Force:         pushForce,
	}

	// Execute push
	result, err := syncer.PushStore(ctx, req)
	if err != nil {
		return err
	}

	if jsonOutput {
		return outputJSON(result)
	}

	// Display result
	if result.DryRun {
		PrintInfo("Dry run - no changes made")
		PrintInfo("")
	}

	if len(result.PushedStores) > 0 {
		if result.DryRun {
			if len(args) == 0 {
				PrintInfo(fmt.Sprintf("Would push all stores (%d):", len(result.PushedStores)))
			} else {
				PrintInfo("Would push stores:")
			}
		} else {
			if len(args) == 0 {
				PrintSuccess(fmt.Sprintf("Pushed all stores (%d):", len(result.PushedStores)))
			} else {
				PrintSuccess("Pushed stores:")
			}
		}
		for _, storeID := range result.PushedStores {
			fmt.Printf("  - %s\n", storeID)
		}
		PrintInfo("")
	}

	if result.PushedWorkspace {
		if result.DryRun {
			PrintInfo("Would push workspace references")
		} else {
			PrintSuccess("Pushed workspace references")
		}
		PrintInfo("")
	}

	if !result.DryRun {
		PrintInfo(fmt.Sprintf("Remote: %s", result.Remote))
		PrintInfo(fmt.Sprintf("Branch: %s", result.Branch))
		PrintInfo(fmt.Sprintf("Commit: %s", result.CommitMessage))
	}

	return nil
}
