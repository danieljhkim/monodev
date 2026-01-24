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
	gitRepo     gitx.GitRepo
	storeRepo   stores.StoreRepo
	stateStore  state.StateStore
	fs          fsops.FS
	hasher      hash.Hasher
	clock       clock.Clock
	configPaths config.Paths
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
		gitRepo:     gitRepo,
		storeRepo:   storeRepo,
		stateStore:  stateStore,
		fs:          fs,
		hasher:      hasher,
		clock:       clk,
		configPaths: paths,
	}
}
