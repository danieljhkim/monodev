package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/sync"
)

var syncAllowSecrets bool

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Commit, push, and pull the active store with the configured remote",
	Long: `sync is the cross-machine verb.

It commits all tracked paths, pushes the result to the configured remote,
then pulls to bring in anything changed elsewhere - resolving the commit
then push then pull ordering so you don't have to know it. Configure a
remote first with 'monodev remote use <name>'.`,
	Args: cobra.NoArgs,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().BoolVar(&syncAllowSecrets, "allow-secrets", false, "Push even when the plaintext persistence payload contains detected secrets")
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	fs := fsops.NewRealFS()
	configStore := remote.NewFileRemoteConfigStore(fs)
	if _, err := configStore.Load(repoRoot); err != nil {
		if err == remote.ErrRemoteNotConfigured {
			return fmt.Errorf("no remote configured; run 'monodev remote use <name>' to configure one before syncing")
		}
		return fmt.Errorf("failed to load remote config: %w", err)
	}

	eng, err := newEngine()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	commitResult, err := eng.Commit(ctx, &engine.CommitRequest{CWD: cwd, All: true})
	if err != nil {
		return err
	}

	syncer, err := newSyncer()
	if err != nil {
		return fmt.Errorf("failed to create syncer: %w", err)
	}

	pushResult, err := syncer.PushStore(ctx, &sync.PushRequest{
		RepoRoot:     repoRoot,
		AllowSecrets: syncAllowSecrets,
	})
	if err != nil {
		return err
	}

	pullResult, err := syncer.PullStore(ctx, &sync.PullRequest{
		RepoRoot: repoRoot,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		result := struct {
			Commit *engine.CommitResult `json:"commit"`
			Push   *sync.PushResult     `json:"push"`
			Pull   *sync.PullResult     `json:"pull"`
		}{Commit: commitResult, Push: pushResult, Pull: pullResult}
		return outputJSON(result)
	}

	printSyncResult(commitResult, pushResult, pullResult)
	return nil
}

func printSyncResult(commitResult *engine.CommitResult, pushResult *sync.PushResult, pullResult *sync.PullResult) {
	PrintSection("Commit")
	PrintSuccess(fmt.Sprintf("Committed %s", PrintCount(len(commitResult.Committed), "path", "paths")))
	if len(commitResult.Removed) > 0 {
		PrintInfo(fmt.Sprintf("Removed %s from store (no longer tracked)", PrintCount(len(commitResult.Removed), "path", "paths")))
	}

	PrintSection("Push")
	if len(pushResult.PushedStores) > 0 {
		PrintSuccess(fmt.Sprintf("Pushed %s to %s:", PrintCount(len(pushResult.PushedStores), "store", "stores"), pushResult.Remote))
		for _, storeID := range pushResult.PushedStores {
			fmt.Printf("  - %s\n", storeID)
		}
	} else {
		PrintInfo("Nothing to push")
	}

	PrintSection("Pull")
	if len(pullResult.PulledStores) > 0 {
		PrintSuccess(fmt.Sprintf("Pulled %s from %s:", PrintCount(len(pullResult.PulledStores), "store", "stores"), pullResult.Remote))
		for _, storeID := range pullResult.PulledStores {
			fmt.Printf("  - %s\n", storeID)
		}
	} else {
		PrintInfo("Nothing new to pull")
	}
	for _, warning := range pullResult.Warnings {
		PrintWarning(warning)
	}
}
