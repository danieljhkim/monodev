package engine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danieljhkim/monodev/internal/state"
)

func TestReplaceManagedExcludeBlockPreservesUserContent(t *testing.T) {
	replacement := managedExcludeBlock([]string{"/Makefile", "/.claude/"})
	for _, contents := range [][]byte{
		[]byte("# user-owned\n/local-cache\n"),
		[]byte("# user-owned without a final newline"),
	} {
		t.Run(string(contents), func(t *testing.T) {
			applied, changed, err := replaceManagedExcludeBlock(contents, replacement)
			if err != nil {
				t.Fatalf("apply managed block: %v", err)
			}
			if !changed {
				t.Fatal("expected adding a managed block to change the file")
			}

			repeated, changed, err := replaceManagedExcludeBlock(applied, replacement)
			if err != nil {
				t.Fatalf("repeat managed block replacement: %v", err)
			}
			if changed || !bytes.Equal(repeated, applied) {
				t.Fatalf("repeated apply changed exclude content:\n first %q\nagain %q", applied, repeated)
			}

			unapplied, changed, err := replaceManagedExcludeBlock(applied, nil)
			if err != nil {
				t.Fatalf("remove managed block: %v", err)
			}
			if !changed {
				t.Fatal("expected removing a managed block to change the file")
			}
			if !bytes.Equal(unapplied, contents) {
				t.Fatalf("user content changed:\n got %q\nwant %q", unapplied, contents)
			}
		})
	}
}

func TestManagedExcludeEntriesAreAnchoredAndSorted(t *testing.T) {
	eng := &Engine{fs: &mockFS{}}
	entries, err := eng.managedExcludeEntries("packages/service", &state.WorkspaceState{Applied: true, Paths: map[string]state.PathOwnership{
		"Makefile": {},
		".claude":  {Contents: &state.DirContents{}},
	}})
	if err != nil {
		t.Fatalf("managed entries: %v", err)
	}
	want := []string{"/packages/service/.claude/", "/packages/service/Makefile"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Fatalf("entries = %v, want %v", entries, want)
		}
	}
}

func TestManagedExcludeEntriesOnlyAppliedOwners(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive-store", true: "active-applied-store"}[applied], func(t *testing.T) {
			eng := &Engine{fs: &mockFS{}}
			ws := state.NewWorkspaceState("repo", "a", "copy")
			ws.Applied = applied
			ws.AddAppliedStore("installed", "copy")
			ws.Paths["secret.txt"] = state.PathOwnership{Store: "installed"}
			ws.Paths["pending.txt"] = state.PathOwnership{Store: "committed-only"}
			entries, err := eng.managedExcludeEntries("a", ws)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"/a/secret.txt"}; !reflect.DeepEqual(entries, want) {
				t.Fatalf("entries = %v, want %v", entries, want)
			}
		})
	}
	eng := &Engine{fs: &mockFS{}}
	ws := state.NewWorkspaceState("repo", "a", "copy")
	ws.Paths["pending.txt"] = state.PathOwnership{Store: "committed-only"}
	entries, err := eng.managedExcludeEntries("a", ws)
	if err != nil || len(entries) != 0 {
		t.Fatalf("committed-only entries = %v, err = %v, want none", entries, err)
	}
	ws.Applied = true
	entries, err = eng.managedExcludeEntries("a", ws)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty applied-store list entries = %v, err = %v, want none", entries, err)
	}
}

func TestExclude_StoreSelectionPreservesInstalledOverlays(t *testing.T) {
	for _, selection := range []string{"use", "create"} {
		for _, reconcile := range []string{"sibling", "linked-worktree"} {
			for _, mode := range []string{"copy", "symlink"} {
				for _, removal := range []string{"unapply", "eject"} {
					t.Run(selection+"/"+reconcile+"/"+mode+"/"+removal, func(t *testing.T) {
						ctx := context.Background()
						repoRoot := setupGitRepo(t)
						runGit(t, repoRoot, "commit", "--allow-empty", "-m", "initial")
						a := filepath.Join(repoRoot, "a")
						b := filepath.Join(repoRoot, "b")
						if reconcile == "linked-worktree" {
							b = filepath.Join(t.TempDir(), "checkout")
							runGit(t, repoRoot, "worktree", "add", "-b", "reconcile", b)
						}
						for _, dir := range []string{a, b} {
							if err := os.MkdirAll(dir, 0700); err != nil {
								t.Fatal(err)
							}
						}
						eng, stateStore, _ := setupIdentityEngine(t)
						commitStore := func(cwd, id, path string) {
							t.Helper()
							if err := eng.CreateStore(ctx, &CreateStoreRequest{CWD: cwd, StoreID: id, Name: id}); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(filepath.Join(cwd, path), []byte(id), 0600); err != nil {
								t.Fatal(err)
							}
							if _, err := eng.Track(ctx, &TrackRequest{CWD: cwd, Paths: []string{path}}); err != nil {
								t.Fatal(err)
							}
							if _, err := eng.Commit(ctx, &CommitRequest{CWD: cwd, All: true}); err != nil {
								t.Fatal(err)
							}
						}
						applyStore := func(cwd, id, applyMode string) {
							t.Helper()
							result, err := eng.Apply(ctx, &ApplyRequest{CWD: cwd, StoreIDs: []string{id}, Mode: applyMode, Force: true})
							if err != nil {
								t.Fatal(err)
							}
							if len(result.Plan.Warnings) != 0 {
								t.Fatalf("apply warnings: %v", result.Plan.Warnings)
							}
						}
						commitStore(b, "reconciler", "sibling.txt")
						commitStore(a, "installed-a", "secret.txt")
						applyStore(b, "reconciler", "copy")
						assertGitExcluded(t, repoRoot, "a/secret.txt", false)
						applyStore(a, "installed-a", mode)
						commitStore(a, "installed-b", "other.txt")
						applyStore(a, "installed-b", mode)
						assertGitExcluded(t, repoRoot, "a/secret.txt", true)
						assertGitExcluded(t, repoRoot, "a/other.txt", true)

						if selection == "use" {
							if err := eng.CreateStore(ctx, &CreateStoreRequest{CWD: b, StoreID: "pending", Name: "pending"}); err != nil {
								t.Fatal(err)
							}
							if err := eng.UseStore(ctx, &UseStoreRequest{CWD: a, StoreID: "pending"}); err != nil {
								t.Fatal(err)
							}
						} else if err := eng.CreateStore(ctx, &CreateStoreRequest{CWD: a, StoreID: "pending", Name: "pending"}); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(a, "pending.txt"), []byte("not applied"), 0600); err != nil {
							t.Fatal(err)
						}
						if _, err := eng.Track(ctx, &TrackRequest{CWD: a, Paths: []string{"pending.txt"}}); err != nil {
							t.Fatal(err)
						}
						if _, err := eng.Commit(ctx, &CommitRequest{CWD: a, All: true}); err != nil {
							t.Fatal(err)
						}
						ws, err := stateStore.LoadWorkspace(state.ComputeWorkspaceID(mustFingerprint(t, repoRoot), "a"))
						if err != nil || ws.Applied || len(ws.AppliedStores) != 2 || len(ws.Paths) != 3 {
							t.Fatalf("selected workspace = %+v, err = %v", ws, err)
						}
						applyStore(b, "reconciler", "copy")
						assertGitExcluded(t, repoRoot, "a/secret.txt", true)
						assertGitExcluded(t, repoRoot, "a/other.txt", true)
						assertGitExcluded(t, repoRoot, "a/pending.txt", false)
						assertFileContent(t, filepath.Join(a, "secret.txt"), "installed-a")
						assertFileContent(t, filepath.Join(a, "other.txt"), "installed-b")

						// Reapplying exercises caller inclusion with Applied=true and
						// committed intent from a different store still in Paths.
						applyStore(a, "installed-a", mode)
						applyStore(b, "reconciler", "copy")
						assertGitExcluded(t, repoRoot, "a/pending.txt", false)
						if removal == "unapply" {
							result, err := eng.Unapply(ctx, &UnapplyRequest{CWD: a, StoreIDs: []string{"installed-a"}})
							if err != nil || len(result.Warnings) != 0 {
								t.Fatalf("selective unapply = %+v, err = %v", result, err)
							}
							applyStore(b, "reconciler", "copy")
							assertGitExcluded(t, repoRoot, "a/secret.txt", false)
							assertGitExcluded(t, repoRoot, "a/other.txt", true)
							result, err = eng.Unapply(ctx, &UnapplyRequest{CWD: a, StoreIDs: []string{"installed-b"}})
							if err != nil || len(result.Warnings) != 0 {
								t.Fatalf("last installed store unapply = %+v, err = %v", result, err)
							}
							applyStore(b, "reconciler", "copy")
							assertGitExcluded(t, repoRoot, "a/other.txt", false)
							assertGitExcluded(t, repoRoot, "a/pending.txt", false)
							assertFileContent(t, filepath.Join(a, "pending.txt"), "not applied")
							result, err = eng.Unapply(ctx, &UnapplyRequest{CWD: a, All: true})
							if err != nil || len(result.Warnings) != 0 {
								t.Fatalf("full unapply = %+v, err = %v", result, err)
							}
							assertFileGone(t, filepath.Join(a, "other.txt"))
						} else {
							result, err := eng.Eject(ctx, &EjectRequest{CWD: a})
							if err != nil || len(result.Warnings) != 0 {
								t.Fatalf("eject = %+v, err = %v", result, err)
							}
							assertFileContent(t, filepath.Join(a, "secret.txt"), "installed-a")
							assertFileContent(t, filepath.Join(a, "other.txt"), "installed-b")
						}
						applyStore(b, "reconciler", "copy")
						assertGitExcluded(t, repoRoot, "a/secret.txt", false)
						assertGitExcluded(t, repoRoot, "a/other.txt", false)
						assertGitExcluded(t, b, "sibling.txt", true)
					})
				}
			}
		}
	}
}

func assertGitExcluded(t *testing.T, repoRoot, path string, want bool) {
	t.Helper()
	cmd := exec.Command("git", "check-ignore", "--no-index", "-q", "--", path)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("git check-ignore %s: %v\n%s", path, err, output)
		}
	}
	if got := err == nil; got != want {
		t.Fatalf("git check-ignore %s = %v, want %v", path, got, want)
	}
}
