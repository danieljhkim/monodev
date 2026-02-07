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

Examples:
  # Pull all stores from remote
  monodev pull

  # Pull a single store
  monodev pull my-store

  # Pull multiple stores
  monodev pull store1 store2

  # Pull and verify checksums
  monodev pull my-store --verify

  # Force pull (overwrite local changes)
  monodev pull my-store --force`,
	Args: cobra.ArbitraryArgs,
	RunE: runPull,
}

var (
	pullRemote string
	pullForce  bool
	pullVerify bool
)

func init() {
	pullCmd.Flags().StringVar(&pullRemote, "remote", "", "Git remote to pull from (defaults to configured remote)")
	pullCmd.Flags().BoolVar(&pullForce, "force", false, "Force pull (overwrite local stores)")
	pullCmd.Flags().BoolVar(&pullVerify, "verify", false, "Verify store integrity with checksums after pulling")
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
		Verify:   pullVerify,
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

	PrintInfo(fmt.Sprintf("Remote: %s", result.Remote))
	PrintInfo(fmt.Sprintf("Branch: %s", result.Branch))

	return nil
}
