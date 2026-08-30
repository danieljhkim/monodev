package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/gitx"
)

var initForce bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize repo-local .monodev directory",
	Long: `Initialize a repo-local .monodev directory at the repository root.

Commands that need a state root auto-create .monodev on first use. init
remains the explicit initializer for scripts and for --force reinit.

This creates .monodev/{stores,workspaces} in the git repository root
(mode 0700) and writes a * .gitignore so artifacts stay local-only.

The home-directory root (~/.monodev) is an opt-in for cross-repo stores:
set MONODEV_ROOT=$HOME/.monodev, or pass --scope global when creating a store.`,
	Args: cobra.NoArgs,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false,
		"Create .monodev even if it already exists (idempotent)")
}

func runInit(cmd *cobra.Command, args []string) error {
	// 1. Discover git repository root
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(cwd)
	if err != nil {
		return config.NotInGitRepositoryError(err)
	}

	// 2. Check if .monodev already exists
	monodevPath := filepath.Join(repoRoot, config.RepoLocalDirName)
	if info, err := os.Stat(monodevPath); err == nil && info.IsDir() {
		if !initForce {
			return fmt.Errorf(".monodev already exists at %s\nUse --force to reinitialize", monodevPath)
		}
		PrintInfo(fmt.Sprintf(".monodev already exists at %s (reinitializing with --force)", monodevPath))
	}

	// 3. Create directory structure and * .gitignore
	if _, err := config.EnsureRepoLocalRoot(repoRoot); err != nil {
		return err
	}

	if _, err := gitx.EnsureDurableRepoID(repoRoot); err != nil {
		return fmt.Errorf("failed to persist durable repo id: %w", err)
	}

	// 4. Display success message
	if jsonOutput {
		result := struct {
			Initialized bool   `json:"initialized"`
			Path        string `json:"path"`
			Message     string `json:"message"`
		}{
			Initialized: true,
			Path:        monodevPath,
			Message:     "All monodev commands in this repo will now use the repo-local .monodev directory",
		}
		return outputJSON(result)
	}

	PrintSuccess(fmt.Sprintf("Initialized .monodev at %s", monodevPath))
	fmt.Println()
	PrintInfo("Next steps:")
	fmt.Println("  1. Create a store:    monodev checkout -n <store-id>")
	fmt.Println("  2. Track files:       monodev track <path>")
	fmt.Println("  3. Commit changes:    monodev commit --all")
	fmt.Println()
	PrintInfo("All monodev commands in this repo will now use the repo-local .monodev directory.")

	return nil
}
