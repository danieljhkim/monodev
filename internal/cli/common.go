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
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// newEngine creates a new engine with real implementations of all dependencies.
func newEngine() (*engine.Engine, error) {
	// Get default paths
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, fmt.Errorf("failed to get config paths: %w", err)
	}

	// Ensure directories exist
	if err := paths.EnsureDirectories(); err != nil {
		return nil, fmt.Errorf("failed to ensure directories: %w", err)
	}

	// Create real implementations
	fs := fsops.NewRealFS()
	gitRepo := gitx.NewRealGitRepo()
	hasher := hash.NewSHA256Hasher()
	clk := &clock.RealClock{}
	stateStore := state.NewFileStateStore(fs, paths.Workspaces, paths.Repos)
	storeRepo := stores.NewFileStoreRepo(fs, paths.Stores)

	// Create engine
	return engine.New(gitRepo, storeRepo, stateStore, fs, hasher, clk, *paths), nil
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
