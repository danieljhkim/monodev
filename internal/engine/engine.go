// Package engine provides the core business logic for monodev operations.
//
// The engine package acts as the orchestration layer between CLI commands and
// lower-level operations. It coordinates workspace discovery, state management,
// store operations, and overlay application/removal.
//
// Key components:
//   - Engine: Main orchestrator that coordinates all operations
//   - Apply/Unapply: Manages overlay application and removal
//   - Track/Commit: Handles tracking and persisting changes
//   - State management: Workspace and store state operations
package engine

import (
	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Engine orchestrates all monodev operations.
// It is the main API surface called by the CLI.
type Engine struct {
	gitRepo             gitx.GitRepo
	storeResolver       *engineStoreResolver
	stateStore          state.StateStore
	componentStateStore state.StateStore
	fs                  fsops.FS
	hasher              hash.Hasher
	clock               clock.Clock
	configPaths         config.Paths
	scopedPaths         *config.ScopedPaths
}

// New creates a new Engine with the given dependencies.
func New(
	gitRepo gitx.GitRepo,
	storeRepo stores.StoreRepo,
	stateStore state.StateStore,
	fs fsops.FS,
	hasher hash.Hasher,
	clk clock.Clock,
	paths config.Paths,
) *Engine {
	return &Engine{
		gitRepo:       gitRepo,
		storeResolver: newEngineStoreResolver(storeRepo, storeRepo, nil),
		stateStore:    stateStore,
		fs:            fs,
		hasher:        hasher,
		clock:         clk,
		configPaths:   paths,
	}
}

// NewScoped creates a new Engine with dual-scope StoreRepo instances.
// Global stores live at ~/.monodev/stores/, component stores at repo_root/.monodev/stores/.
func NewScoped(
	gitRepo gitx.GitRepo,
	scopedPaths *config.ScopedPaths,
	fs fsops.FS,
	hasher hash.Hasher,
	clk clock.Clock,
) *Engine {
	globalStateStore := state.NewFileStateStore(fs, scopedPaths.Global.Workspaces)
	var componentStateStore state.StateStore
	if scopedPaths.Component != nil {
		componentStateStore = state.NewFileStateStore(fs, scopedPaths.Component.Workspaces)
	}

	return &Engine{
		gitRepo:             gitRepo,
		storeResolver:       newScopedEngineStoreResolver(fs, scopedPaths),
		stateStore:          globalStateStore,
		componentStateStore: componentStateStore,
		fs:                  fs,
		hasher:              hasher,
		clock:               clk,
		configPaths:         *scopedPaths.Global,
		scopedPaths:         scopedPaths,
	}
}
