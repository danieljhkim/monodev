package cli

import (
	"context"
	"fmt"

	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/sync"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [store-id...]",
	Short: "Pull stores from remote persistence repository",
	Long: `Pull stores from the remote persistence repository.

Fetches stores from the separate Git orphan branch at monodev/persist
and restores them to ~/.monodev/stores/.

If no store IDs are specified, pulls all stores from the remote.

Every pull verifies checksums against the persisted manifest and warns
when a store has no manifest to check. If a store already exists locally
and the pulled content differs from that local copy, the pull is refused
and the changed paths are reported; pass --force to overwrite anyway.

Examples:
  # Pull all stores from remote
  monodev pull

  # Pull a single store
  monodev pull my-store

  # Pull multiple stores
  monodev pull store1 store2

  # Force pull (overwrite local changes)
  monodev pull my-store --force`,
	Args: cobra.ArbitraryArgs,
	RunE: runPull,
}

var (
	pullRemote string
	pullForce  bool
)

func init() {
	pullCmd.Flags().StringVar(&pullRemote, "remote", "", "Git remote to pull from (defaults to configured remote)")
	pullCmd.Flags().BoolVar(&pullForce, "force", false, "Force pull, overwriting a local store whose content differs from what is being pulled")
}

func runPull(cmd *cobra.Command, args []string) error {
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
	req := &sync.PullRequest{
		RepoRoot: repoRoot,
		StoreIDs: args,
		Remote:   pullRemote,
		Force:    pullForce,
	}

	// Execute pull
	result, err := syncer.PullStore(ctx, req)
	if err != nil {
		return err
	}

	if jsonOutput {
		return outputJSON(result)
	}

	// Display result
	if len(result.PulledStores) > 0 {
		if len(args) == 0 {
			PrintSuccess(fmt.Sprintf("Pulled all stores (%d):", len(result.PulledStores)))
		} else {
			PrintSuccess("Pulled stores:")
		}
		for _, storeID := range result.PulledStores {
			fmt.Printf("  - %s\n", storeID)
		}
		PrintInfo("")
	} else {
		PrintInfo("No stores found in remote")
	}

	if result.Verified {
		PrintSuccess("All stores verified successfully")
		PrintInfo("")
	}

	for _, warning := range result.Warnings {
		PrintWarning(warning)
	}
	if len(result.Warnings) > 0 {
		PrintInfo("")
	}

	PrintInfo(fmt.Sprintf("Remote: %s", result.Remote))
	PrintInfo(fmt.Sprintf("Branch: %s", result.Branch))

	return nil
}
