package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
	"github.com/danieljhkim/monodev/internal/sync"
)

// newEngine creates a new engine with real implementations of all dependencies.
func newEngine() (*engine.Engine, error) {
	scopedPaths, err := prepareScopedPaths()
	if err != nil {
		return nil, err
	}

	fs := fsops.NewRealFS()
	gitRepo := gitx.NewRealGitRepo()
	hasher := hash.NewSHA256Hasher()
	clk := &clock.RealClock{}

	return engine.NewScoped(gitRepo, scopedPaths, fs, hasher, clk), nil
}

// newSyncer creates a new syncer with real implementations of all dependencies.
// Path resolution matches newEngine: repo-local .monodev by default (auto-created
// on first use), plus ~/.monodev (or MONODEV_ROOT) so existing home stores stay
// reachable.
func newSyncer() (*sync.Syncer, error) {
	scopedPaths, err := prepareScopedPaths()
	if err != nil {
		return nil, err
	}

	fs := fsops.NewRealFS()
	hasher := hash.NewSHA256Hasher()
	clk := &clock.RealClock{}
	storeRepo, stateStore := scopedSyncerRepos(fs, scopedPaths)
	gitPersist := remote.NewRealGitPersistence()
	configStore := remote.NewFileRemoteConfigStore(fs)
	snapshotMgr := persist.NewSnapshotManager(fs)

	return sync.New(gitPersist, storeRepo, stateStore, snapshotMgr, configStore, fs, hasher, clk), nil
}

// prepareScopedPaths auto-creates repo-local .monodev when needed, then
// ensures both scopes' directories exist. When inside a git repository it
// also persists a durable repo ID, matching `monodev init`.
func prepareScopedPaths() (*config.ScopedPaths, error) {
	scopedPaths, err := config.EnsureScopedPaths()
	if err != nil {
		return nil, err
	}

	if err := scopedPaths.EnsureDirectories(); err != nil {
		return nil, fmt.Errorf("failed to ensure directories: %w", err)
	}

	if scopedPaths.RepoRoot != "" {
		if _, err := gitx.EnsureDurableRepoID(scopedPaths.RepoRoot); err != nil {
			return nil, fmt.Errorf("failed to persist durable repo id: %w", err)
		}
	}

	return scopedPaths, nil
}

// scopedSyncerRepos builds store and workspace repos that share the engine's
// dual-scope roots: workspace state is loaded from global then component and
// saved to global for new IDs; stores are found in either scope and created
// in the repo-local component scope when it is present.
func scopedSyncerRepos(fs fsops.FS, scopedPaths *config.ScopedPaths) (stores.StoreRepo, state.StateStore) {
	globalStores := stores.NewFileStoreRepo(fs, scopedPaths.Global.Stores)
	globalState := state.NewFileStateStore(fs, scopedPaths.Global.Workspaces)
	if scopedPaths.Component == nil {
		return globalStores, globalState
	}
	componentStores := stores.NewFileStoreRepo(fs, scopedPaths.Component.Stores)
	componentState := state.NewFileStateStore(fs, scopedPaths.Component.Workspaces)
	return stores.NewScopedRepo(globalStores, componentStores), state.NewScopedStore(globalState, componentState)
}

// formatJSON formats a value as JSON.
func formatJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// formatError formats an error for display.
func formatError(err error) string {
	initColors()
	return errorColor.Sprintf("Error: %v", err)
}

// outputJSON outputs a value as JSON to stdout.
func outputJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Note: PrintSuccess, PrintWarning, PrintError, and PrintInfo are now
// defined in format.go with enhanced formatting and colors.
