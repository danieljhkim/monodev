// Package config manages monodev configuration and filesystem paths.
//
// Configuration includes the locations of monodev data directories, which can
// be customized via environment variables. The default root is ~/.monodev/
// containing stores/, workspaces/, and config files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths contains all the filesystem paths used by monodev.
type Paths struct {
	// Root is the base directory for all monodev data (default: ~/.monodev)
	Root string

	// Stores is the directory containing all store data
	Stores string

	// Workspaces is the directory containing workspace state files
	Workspaces string

	// Config is the path to the global config file
	Config string
}

// DefaultPaths returns the default paths for monodev.
// Paths can be overridden with environment variables:
// - MONODEV_ROOT: Override the root directory
func DefaultPaths() (*Paths, error) {
	root := os.Getenv("MONODEV_ROOT")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		root = filepath.Join(home, ".monodev")
	}

	return &Paths{
		Root:       root,
		Stores:     filepath.Join(root, "stores"),
		Workspaces: filepath.Join(root, "workspaces"),
		Config:     filepath.Join(root, "config.yaml"),
	}, nil
}

// EnsureDirectories creates all necessary directories if they don't exist.
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Root,
		p.Stores,
		p.Workspaces,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}
