package cli

import (
	"fmt"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/spf13/cobra"
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage remote persistence configuration",
	Long: `Manage remote persistence configuration for pushing and pulling stores.

The remote configuration is stored repo-locally at .monodev/remote.json
and specifies which Git remote to use and which branch to use for persistence.`,
}

var remoteUseCmd = &cobra.Command{
	Use:   "use <remote-name>",
	Short: "Select a Git remote for persistence",
	Long: `Select a Git remote to use for pushing and pulling stores.

The remote must exist in the main repository's git configuration.
This command verifies the remote exists before saving the configuration.

Examples:
  # Use the origin remote
  monodev remote use origin

  # Use a custom remote
  monodev remote use upstream`,
	Args: cobra.ExactArgs(1),
	RunE: runRemoteUse,
}

var remoteSetBranchCmd = &cobra.Command{
	Use:   "set-branch <branch>",
	Short: "Set the persistence branch name",
	Long: `Set the orphan branch name to use for persistence.

The default branch is monodev/persist. You can customize this
if you need to use a different branch name.

Examples:
  # Use custom branch name
  monodev remote set-branch monodev/custom

  # Restore default branch
  monodev remote set-branch monodev/persist`,
	Args: cobra.ExactArgs(1),
	RunE: runRemoteSetBranch,
}

var remoteShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current remote configuration",
	Long: `Display the current remote persistence configuration.

Shows the configured Git remote, branch name, and last update time.`,
	Args: cobra.NoArgs,
	RunE: runRemoteShow,
}

func init() {
	remoteCmd.AddCommand(remoteUseCmd)
	remoteCmd.AddCommand(remoteSetBranchCmd)
	remoteCmd.AddCommand(remoteShowCmd)
}

func runRemoteUse(cmd *cobra.Command, args []string) error {
	remoteName := args[0]

	// Get the repository root
	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Verify the remote exists in the main repository
	gitPersist := remote.NewRealGitPersistence()
	remoteURL, err := gitPersist.GetRemoteURL(repoRoot, remoteName)
	if err != nil {
		return fmt.Errorf("remote %q not found in repository: %w", remoteName, err)
	}

	// Load or create config
	fs := fsops.NewRealFS()
	configStore := remote.NewFileRemoteConfigStore(fs)

	config, err := configStore.Load(repoRoot)
	if err != nil {
		if err == remote.ErrRemoteNotConfigured {
			// Create new config
			config = remote.DefaultRemoteConfig()
		} else {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Update remote
	config.Remote = remoteName

	// Save config
	if err := configStore.Save(repoRoot, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	PrintSuccess(fmt.Sprintf("Remote set to %q", remoteName))
	PrintInfo(fmt.Sprintf("URL: %s", remoteURL))
	PrintInfo(fmt.Sprintf("Branch: %s", config.Branch))

	return nil
}

func runRemoteSetBranch(cmd *cobra.Command, args []string) error {
	branchName := args[0]

	// Get the repository root
	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Load or create config
	fs := fsops.NewRealFS()
	configStore := remote.NewFileRemoteConfigStore(fs)

	config, err := configStore.Load(repoRoot)
	if err != nil {
		if err == remote.ErrRemoteNotConfigured {
			// Create new config
			config = remote.DefaultRemoteConfig()
		} else {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Update branch
	config.Branch = branchName

	// Save config
	if err := configStore.Save(repoRoot, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	PrintSuccess(fmt.Sprintf("Branch set to %q", branchName))
	PrintInfo(fmt.Sprintf("Remote: %s", config.Remote))

	return nil
}

func runRemoteShow(cmd *cobra.Command, args []string) error {
	// Get the repository root
	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Load config
	fs := fsops.NewRealFS()
	configStore := remote.NewFileRemoteConfigStore(fs)

	config, err := configStore.Load(repoRoot)
	if err != nil {
		if err == remote.ErrRemoteNotConfigured {
			PrintWarning("Remote not configured")
			PrintInfo("Run 'monodev remote use <name>' to configure a remote")
			return nil
		}
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get remote URL
	gitPersist := remote.NewRealGitPersistence()
	remoteURL, err := gitPersist.GetRemoteURL(repoRoot, config.Remote)
	if err != nil {
		PrintWarning(fmt.Sprintf("Remote %q not found in repository", config.Remote))
		remoteURL = "(not found)"
	}

	// Display config
	fmt.Printf("Remote:  %s\n", config.Remote)
	fmt.Printf("URL:     %s\n", remoteURL)
	fmt.Printf("Branch:  %s\n", config.Branch)
	fmt.Printf("Updated: %s\n", config.UpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}
