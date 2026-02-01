package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/gitx"
)

var initForce bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize repo-local .monodev directory",
	Long: `Initialize a repo-local .monodev directory at the repository root.

This creates .monodev/{stores,workspaces} in the git repository root,
enabling repo-scoped monodev configuration instead of using ~/.monodev.

The .monodev directory is automatically added to .gitignore to keep
it local-only and not committed to the repository.`,
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
		return fmt.Errorf("not in a git repository: %w\nmonodev init must be run inside a git repository", err)
	}

	// 2. Check if .monodev already exists
	monodevPath := filepath.Join(repoRoot, ".monodev")
	if info, err := os.Stat(monodevPath); err == nil && info.IsDir() {
		if !initForce {
			return fmt.Errorf(".monodev already exists at %s\nUse --force to reinitialize", monodevPath)
		}
		PrintInfo(fmt.Sprintf(".monodev already exists at %s (reinitializing with --force)", monodevPath))
	}

	// 3. Create directory structure
	dirs := []string{
		monodevPath,
		filepath.Join(monodevPath, "stores"),
		filepath.Join(monodevPath, "workspaces"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// 4. Create .gitignore to exclude .monodev from git
	gitignorePath := filepath.Join(monodevPath, ".gitignore")
	gitignoreContent := []byte("# monodev artifacts (local-only)\n*\n")
	if err := os.WriteFile(gitignorePath, gitignoreContent, 0644); err != nil {
		return fmt.Errorf("failed to create .gitignore: %w", err)
	}

	// 5. Display success message
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
