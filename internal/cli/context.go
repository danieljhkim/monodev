package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/sync"
)

// Conventional agent paths inside a workspace. They are plain files in the
// store overlay; monodev only lists them.
var (
	agentNotesDir    = filepath.Join(".agents", "notes")
	agentSessionsDir = filepath.Join(".agents", "notes", "sessions")
	agentScriptsDir  = filepath.Join(".agents", "scripts")
)

// Values of contextResult.Source.
const (
	contextSourceActiveStore     = "active-store"
	contextSourceLocalReference  = "local-reference"
	contextSourceRemoteReference = "remote-reference"
	contextSourceNone            = "none"
)

// contextResult is the documented --json shape of 'monodev context'. Every
// field is always present; lists are empty rather than null.
type contextResult struct {
	WorkspaceID   string   `json:"workspaceId"`
	WorkspacePath string   `json:"workspacePath"`
	Source        string   `json:"source"`
	ActiveStore   string   `json:"activeStore"`
	Stores        []string `json:"stores"`
	PulledStores  []string `json:"pulledStores"`
	Notes         []string `json:"notes"`
	Sessions      []string `json:"sessions"`
	Scripts       []string `json:"scripts"`
	Warnings      []string `json:"warnings"`
	Hint          string   `json:"hint"`
}

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Find and restore agent context for this directory",
	Long: `Find and restore agent context for the current directory.

Run it at the start of an agent session. If this workspace has an active
store, context reports it. Otherwise it looks for a workspace reference for
this directory, first in the local persistence work tree, then on the
configured remote's persistence branch. A match is pulled with its stores and
applied through the normal pull and apply checks; context never forces past
a conflict or drift refusal, and never pushes.

It then lists the conventional agent paths on disk:
  .agents/notes/            notes, such as lessons.md
  .agents/notes/sessions/   one file per session
  .agents/scripts/          helper scripts

No remote, offline, or nothing found is not an error: context exits 0 with an
empty result and a hint.`,
	Args: cobra.NoArgs,
	RunE: runContext,
}

func runContext(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	eng, err := newEngine()
	if err != nil {
		return err
	}
	status, err := eng.Status(ctx, &engine.StatusRequest{CWD: cwd})
	if err != nil {
		return err
	}

	result := &contextResult{
		WorkspaceID:   status.WorkspaceID,
		WorkspacePath: status.WorkspacePath,
		Source:        contextSourceNone,
		Stores:        []string{},
		PulledStores:  []string{},
		Warnings:      []string{},
	}

	if status.ActiveStore != "" {
		result.Source = contextSourceActiveStore
		result.ActiveStore = status.ActiveStore
		result.Stores = appendUniqueString(append([]string{}, status.AppliedStores...), status.ActiveStore)
		if status.ActiveStoreStatus != "Applied" {
			result.Hint = fmt.Sprintf("Active store %s is not fully applied; run 'monodev apply' to restore its files.", status.ActiveStore)
		}
	} else if err := restoreContext(ctx, eng, cwd, result); err != nil {
		return err
	}

	if err := listAgentPaths(cwd, result); err != nil {
		return err
	}
	if result.Source == contextSourceNone && result.Hint == "" {
		result.Hint = "No monodev context for this directory; start one with 'monodev checkout -n <store>'."
	}

	if jsonOutput {
		return outputJSON(result)
	}
	printContextResult(result)
	return nil
}

// restoreContext looks for a workspace reference for this directory and, when
// one is found, pulls and applies its stores. Absence, a missing remote, and
// an unreachable remote leave result empty with a hint; refusals from pull or
// apply are returned as errors.
func restoreContext(ctx context.Context, eng *engine.Engine, cwd string, result *contextResult) error {
	gitRepo := gitx.NewRealGitRepo()
	repoRoot, err := gitRepo.Discover(cwd)
	if err != nil {
		result.Hint = "Not in a git repository; monodev context needs one to find shared context."
		return nil
	}
	identity, err := localWorkspaceIdentity(gitRepo, repoRoot, cwd)
	if err != nil {
		result.Hint = fmt.Sprintf("Cannot match shared context: %v.", err)
		return nil
	}

	syncer, err := newSyncer()
	if err != nil {
		return fmt.Errorf("failed to create syncer: %w", err)
	}

	source := contextSourceLocalReference
	match, err := syncer.FindWorkspaceReference(repoRoot, identity.repositoryIdentity, identity.workspacePath)
	if err != nil {
		return err
	}
	if match == nil {
		source = contextSourceRemoteReference
		if _, _, fetchErr := syncer.FetchPersistence(ctx, repoRoot, ""); fetchErr != nil {
			if errors.Is(fetchErr, remote.ErrRemoteNotConfigured) {
				result.Hint = "No monodev remote configured; run 'monodev remote use origin' to look for shared context."
			} else {
				result.Warnings = append(result.Warnings, fetchErr.Error())
				result.Hint = "Could not fetch the persistence branch (offline, or nothing pushed yet)."
			}
			return nil
		}
		match, err = syncer.FindWorkspaceReference(repoRoot, identity.repositoryIdentity, identity.workspacePath)
		if err != nil {
			return err
		}
		if match == nil {
			return nil
		}
	}

	pullResult, err := syncer.PullStore(ctx, &sync.PullRequest{
		RepoRoot:           repoRoot,
		StoreIDs:           match.StoreIDs,
		WorkspaceID:        match.WorkspaceID,
		LocalWorkspaceID:   identity.localWorkspaceID,
		RepoFingerprint:    identity.fingerprint,
		RepositoryIdentity: identity.repositoryIdentity,
		WorkspacePath:      identity.workspacePath,
		WithStores:         true,
		SkipFetch:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to restore workspace reference %s: %w", match.WorkspaceID, err)
	}
	result.Source = source
	result.PulledStores = nonNilStrings(pullResult.PulledStores)
	result.Warnings = append(result.Warnings, pullResult.Warnings...)
	result.ActiveStore = match.ActiveStore
	result.Stores = appendUniqueString(append([]string{}, match.AppliedStores...), match.ActiveStore)

	if len(match.AppliedStores) == 0 && match.ActiveStore == "" {
		return nil
	}
	applyResult, err := eng.Apply(ctx, &engine.ApplyRequest{
		CWD:      cwd,
		Mode:     "copy",
		StoreIDs: append([]string{}, match.AppliedStores...),
	})
	if err != nil {
		if applyResult != nil && applyResult.Plan != nil {
			for _, conflict := range applyResult.Plan.Conflicts {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", conflict.Path, conflict.Reason))
			}
			if !jsonOutput {
				for _, conflict := range applyResult.Plan.Conflicts {
					PrintError(fmt.Sprintf("%s: %s", conflict.Path, conflict.Reason))
				}
			}
		}
		return fmt.Errorf("stores pulled but not applied: %w; resolve the conflicts, then run 'monodev apply'", err)
	}
	if applyResult.Plan != nil {
		result.Warnings = append(result.Warnings, applyResult.Plan.Warnings...)
	}
	// Applying by store ID makes the last one active; put back the store the
	// reference recorded as active.
	if n := len(match.AppliedStores); n > 0 && match.ActiveStore != "" && match.AppliedStores[n-1] != match.ActiveStore {
		if err := eng.UseStore(ctx, &engine.UseStoreRequest{CWD: cwd, StoreID: match.ActiveStore}); err != nil {
			return err
		}
	}
	return nil
}

type workspaceIdentity struct {
	localWorkspaceID   string
	fingerprint        string
	repositoryIdentity string
	workspacePath      string
}

// localWorkspaceIdentity computes the identity a persisted workspace reference
// is validated against when it is restored into this checkout.
func localWorkspaceIdentity(gitRepo gitx.GitRepo, repoRoot, cwd string) (*workspaceIdentity, error) {
	fingerprint, err := gitRepo.Fingerprint(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to get local repository fingerprint: %w", err)
	}
	_, repositoryIdentity, err := gitRepo.GetFingerprintComponents(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to determine portable repository identity: %w", err)
	}
	if repositoryIdentity == "" {
		return nil, fmt.Errorf("failed to determine portable repository identity: no remote origin URL")
	}
	workspacePath, err := gitRepo.RelPath(repoRoot, cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to compute workspace path: %w", err)
	}
	return &workspaceIdentity{
		localWorkspaceID:   state.ComputeWorkspaceID(fingerprint, workspacePath),
		fingerprint:        fingerprint,
		repositoryIdentity: repositoryIdentity,
		workspacePath:      workspacePath,
	}, nil
}

// listAgentPaths records the regular files under the conventional agent
// directories, relative to the workspace root and slash-separated.
func listAgentPaths(workspaceRoot string, result *contextResult) error {
	var err error
	if result.Sessions, err = listFiles(workspaceRoot, agentSessionsDir, ""); err != nil {
		return err
	}
	if result.Notes, err = listFiles(workspaceRoot, agentNotesDir, agentSessionsDir); err != nil {
		return err
	}
	if result.Scripts, err = listFiles(workspaceRoot, agentScriptsDir, ""); err != nil {
		return err
	}
	return nil
}

func listFiles(workspaceRoot, dir, skipDir string) ([]string, error) {
	files := []string{}
	root := filepath.Join(workspaceRoot, dir)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(workspaceRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir != "" && rel == skipDir {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func printContextResult(result *contextResult) {
	PrintSection("Agent Context")
	PrintLabelValue("Workspace Path", result.WorkspacePath)
	PrintLabelValue("Source", result.Source)
	if result.ActiveStore != "" {
		PrintLabelValue("Active Store", result.ActiveStore)
	}
	if len(result.Stores) > 0 {
		PrintLabelValue("Stores", strings.Join(result.Stores, ", "))
	}
	if len(result.PulledStores) > 0 {
		PrintLabelValue("Pulled", strings.Join(result.PulledStores, ", "))
	}

	for _, group := range []struct {
		title string
		paths []string
	}{
		{title: "Notes:", paths: result.Notes},
		{title: "Sessions:", paths: result.Sessions},
		{title: "Scripts:", paths: result.Scripts},
	} {
		if len(group.paths) == 0 {
			continue
		}
		fmt.Println()
		PrintSubsection(group.title)
		PrintList(group.paths, 1)
	}

	if len(result.Warnings) > 0 {
		fmt.Println()
		for _, warning := range result.Warnings {
			PrintWarning(warning)
		}
	}
	if result.Hint != "" {
		fmt.Println()
		PrintInfo(result.Hint)
	}
}
