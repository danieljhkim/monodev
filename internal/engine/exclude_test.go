package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
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

func TestManagedExcludePatternEncodesExactPath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		directory bool
		want      string
		wantErr   bool
	}{
		{name: "trailing space", path: "notes ", want: `/notes\ `},
		{name: "two trailing spaces", path: "notes  ", want: `/notes\ \ `},
		{name: "directory trailing space", path: "notes ", directory: true, want: `/notes\ /`},
		{name: "interior space", path: "my notes", want: "/my notes"},
		{name: "asterisk", path: "file*.txt", want: `/file\*.txt`},
		{name: "question mark", path: "file?.txt", want: `/file\?.txt`},
		{name: "brackets", path: "a[b].txt", want: `/a\[b\].txt`},
		{name: "backslash", path: `has\x`, want: `/has\\x`},
		{name: "backslash and trailing space", path: "a\\ ", want: `/a\\\ `},
		{name: "interior carriage return", path: "foo\rbar", want: "/foo\rbar"},
		{name: "directory trailing carriage return", path: "dir\r", directory: true, want: "/dir\r/"},
		{name: "trailing tab", path: "tab\t", want: "/tab\t"},
		{name: "newline", path: "prefix\nsecret.private", wantErr: true},
		{name: "nul", path: "has\x00nul", wantErr: true},
		{name: "file trailing carriage return", path: "foo\r", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := managedExcludePattern(tt.path, tt.directory)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("managedExcludePattern(%q) err = %v, want validation error", tt.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("managedExcludePattern(%q) err = %v", tt.path, err)
			}
			if got != tt.want {
				t.Fatalf("managedExcludePattern(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}

	eng := &Engine{fs: &mockFS{}}
	entries, err := eng.managedExcludeEntries("packages/service", &state.WorkspaceState{
		Applied: true,
		Paths: map[string]state.PathOwnership{
			"notes ":    {},
			"file*.txt": {},
			"has\\x":    {},
			"spacedir ": {Contents: &state.DirContents{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`/packages/service/file\*.txt`,
		`/packages/service/has\\x`,
		`/packages/service/notes\ `,
		`/packages/service/spacedir\ /`,
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}

	_, err = eng.managedExcludeEntries(".", &state.WorkspaceState{
		Applied: true,
		Paths:   map[string]state.PathOwnership{"prefix\nsecret.private": {}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("newline ledger path err = %v, want validation error", err)
	}
}

func TestManagedExcludeCheckIgnoreSelectsExactPath(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	runGit(t, repoRoot, "commit", "--allow-empty", "-m", "initial")
	eng, _, _ := setupIdentityEngine(t)

	managed := []string{
		"notes ",
		"notes  ",
		"file*.txt",
		"file?.txt",
		"a[b].txt",
		"has\\x",
		"foo\rbar",
		"my notes",
	}
	sentinels := []string{
		"notes",
		"notes   ",
		"fileX.txt",
		"fileQ.txt",
		"ab.txt",
		"hasx",
		"foobar",
		"secret.private",
		"prefix",
	}
	dirName := "spacedir "
	crDir := "dir\r"
	storeID := "exact-exclude"
	if err := eng.CreateStore(ctx, &CreateStoreRequest{CWD: repoRoot, StoreID: storeID, Name: storeID}); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(append([]string{}, managed...), sentinels...) {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte("visible"), 0600); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	for _, dir := range []string{dirName, crDir} {
		if err := os.MkdirAll(filepath.Join(repoRoot, dir), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoRoot, dir, "child"), []byte("child"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "dir"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}

	trackPaths := append(append([]string{}, managed...), dirName, crDir)
	if _, err := eng.Track(ctx, &TrackRequest{CWD: repoRoot, Paths: trackPaths}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Commit(ctx, &CommitRequest{CWD: repoRoot, All: true}); err != nil {
		t.Fatal(err)
	}
	result, err := eng.Apply(ctx, &ApplyRequest{CWD: repoRoot, StoreIDs: []string{storeID}, Mode: "copy", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan != nil && len(result.Plan.Warnings) != 0 {
		t.Fatalf("apply warnings: %v", result.Plan.Warnings)
	}

	for _, name := range trackPaths {
		directory := name == dirName || name == crDir
		want, err := managedExcludePattern(name, directory)
		if err != nil {
			t.Fatal(err)
		}
		if got := gitCheckIgnorePattern(t, repoRoot, name); got != want {
			t.Fatalf("check-ignore %q pattern = %q, want %q", name, got, want)
		}
	}
	for _, name := range append(sentinels, "dir") {
		if got := gitCheckIgnorePattern(t, repoRoot, name); got != "" {
			t.Fatalf("sentinel %q matched %q, want it to stay visible", name, got)
		}
	}
}

func TestUnrepresentableManagedPathRefusedBeforeMutation(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	runGit(t, repoRoot, "commit", "--allow-empty", "-m", "initial")
	eng, stateStore, _ := setupIdentityEngine(t)
	storeID := "bad-names"
	if err := eng.CreateStore(ctx, &CreateStoreRequest{CWD: repoRoot, StoreID: storeID, Name: storeID}); err != nil {
		t.Fatal(err)
	}

	bad := "prefix\nsecret.private"
	trailingCR := "foo\r"
	for _, name := range []string{"prefix", "secret.private", "foo", "ok.txt"} {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte("visible"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	excludeBefore := readExcludeFile(t, repoRoot)
	trackPath := filepath.Join(eng.configPaths.Stores, storeID, "track.json")
	trackBefore, err := os.ReadFile(trackPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, bad), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Track(ctx, &TrackRequest{CWD: repoRoot, Paths: []string{"ok.txt", bad}}); err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("track newline err = %v, want validation error", err)
	}
	trackAfter, err := os.ReadFile(trackPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(trackBefore, trackAfter) {
		t.Fatalf("track file changed on refused track:\n before %q\n after %q", trackBefore, trackAfter)
	}

	repo := stores.NewFileStoreRepo(fsops.NewRealFS(), eng.configPaths.Stores)
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{
		{Path: "ok.txt", Kind: "file"},
		{Path: bad, Kind: "file"},
		{Path: trailingCR, Kind: "file"},
	}
	if err := repo.SaveTrack(storeID, track); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, trailingCR), []byte("cr"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Commit(ctx, &CommitRequest{CWD: repoRoot, All: true}); err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("commit err = %v, want validation error", err)
	}
	for _, name := range []string{"ok.txt", bad, trailingCR} {
		if _, statErr := os.Lstat(filepath.Join(repo.OverlayRoot(storeID), name)); !os.IsNotExist(statErr) {
			t.Fatalf("commit wrote store overlay %q: %v", name, statErr)
		}
	}
	assertLedgerOmits(t, stateStore, repoRoot, "ok.txt", bad, trailingCR)
	if got := readExcludeFile(t, repoRoot); !bytes.Equal(got, excludeBefore) {
		t.Fatalf("exclude changed on refused commit:\n got %q\nwant %q", got, excludeBefore)
	}

	for _, name := range []string{"ok.txt", bad, trailingCR} {
		path := filepath.Join(repo.OverlayRoot(storeID), name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("overlay"), 0600); err != nil {
			t.Fatalf("seed overlay %q: %v", name, err)
		}
	}
	if _, err := eng.Apply(ctx, &ApplyRequest{CWD: repoRoot, StoreIDs: []string{storeID}, Mode: "copy", Force: true}); err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("apply err = %v, want validation error", err)
	}
	assertFileContent(t, filepath.Join(repoRoot, "ok.txt"), "visible")
	assertFileContent(t, filepath.Join(repoRoot, bad), "secret")
	assertFileContent(t, filepath.Join(repoRoot, trailingCR), "cr")
	assertFileContent(t, filepath.Join(repoRoot, "prefix"), "visible")
	assertFileContent(t, filepath.Join(repoRoot, "secret.private"), "visible")
	assertFileContent(t, filepath.Join(repoRoot, "foo"), "visible")
	assertLedgerOmits(t, stateStore, repoRoot, "ok.txt", bad, trailingCR)
	if got := readExcludeFile(t, repoRoot); !bytes.Equal(got, excludeBefore) {
		t.Fatalf("exclude changed on refused apply:\n got %q\nwant %q", got, excludeBefore)
	}
	for _, name := range []string{"prefix", "secret.private", "foo", "ok.txt"} {
		if got := gitCheckIgnorePattern(t, repoRoot, name); got != "" {
			t.Fatalf("sentinel %q matched %q after refused apply", name, got)
		}
	}
}

func gitCheckIgnorePattern(t *testing.T, repoRoot, path string) string {
	t.Helper()
	cmd := exec.Command("git", "check-ignore", "--no-index", "-v", "--", path)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return ""
		}
		t.Fatalf("git check-ignore %q: %v\n%s", path, err, output)
	}
	line := strings.TrimRight(string(output), "\n")
	tab := strings.LastIndex(line, "\t")
	if tab < 0 {
		t.Fatalf("git check-ignore %q output = %q", path, output)
	}
	meta := line[:tab]
	colon := strings.LastIndex(meta, ":")
	if colon < 0 {
		t.Fatalf("git check-ignore %q output = %q", path, output)
	}
	return meta[colon+1:]
}

func readExcludeFile(t *testing.T, repoRoot string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, ".git", "info", "exclude"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertLedgerOmits(t *testing.T, stateStore *state.FileStateStore, repoRoot string, paths ...string) {
	t.Helper()
	ws, err := stateStore.LoadWorkspace(state.ComputeWorkspaceID(mustFingerprint(t, repoRoot), "."))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, ok := ws.Paths[path]; ok {
			t.Fatalf("ledger contains %q", path)
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
