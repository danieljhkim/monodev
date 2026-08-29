package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

const concurrencyHelperEnv = "MONODEV_CONCURRENCY_HELPER"

func TestConcurrentWorkspaceAndStoreMutationsSerializeAcrossProcesses(t *testing.T) {
	if os.Getenv(concurrencyHelperEnv) == "1" {
		runConcurrencyHelper(t)
		return
	}

	root := t.TempDir()
	workspacesDir := filepath.Join(root, "workspaces")
	storesDir := filepath.Join(root, "stores")
	workspaceDir := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceDir, 0700); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	stateRepo := state.NewFileStateStore(fs, workspacesDir)
	storeRepo := stores.NewFileStoreRepo(fs, storesDir)
	workspaceID := state.ComputeWorkspaceID("repo", ".")
	storeID := "shared"
	ws := state.NewWorkspaceState("repo", ".", "copy")
	ws.ActiveStore = storeID
	ws.ActiveStoreScope = stores.ScopeGlobal
	if err := stateRepo.SaveWorkspace(workspaceID, ws); err != nil {
		t.Fatal(err)
	}
	meta := stores.NewStoreMeta("Shared", stores.ScopeGlobal, time.Now())
	if err := storeRepo.Create(storeID, meta); err != nil {
		t.Fatal(err)
	}
	for _, writer := range []string{"writer-one", "writer-two"} {
		if err := os.WriteFile(filepath.Join(workspaceDir, writer+".txt"), []byte(writer), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Track mutates track.json and meta.json. Hold the production store lock so
	// both command processes reach the same contention point before release.
	storeGate, err := storeRepo.LockStore(context.Background(), storeID, lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	runConflictingHelpers(t, root, "track", storeGate.Close)

	gotTrack, err := storeRepo.LoadTrack(storeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range []string{"writer-one", "writer-two"} {
		if !trackContains(gotTrack, writer+".txt") {
			t.Errorf("concurrent Track lost %s", writer)
		}
	}

	// Commit mutates workspace files, the store overlay/meta, and workspace
	// ownership. Gate the workspace lock, then prove both command processes are
	// blocked and ultimately preserve both writers after serial execution.
	workspaceGate, err := stateRepo.LockWorkspace(context.Background(), workspaceID, lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	runConflictingHelpers(t, root, "commit", workspaceGate.Close)

	gotWorkspace, err := stateRepo.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range []string{"writer-one", "writer-two"} {
		path := writer + ".txt"
		if _, ok := gotWorkspace.Paths[path]; !ok {
			t.Errorf("workspace ownership lost %s", path)
		}
		if data, err := os.ReadFile(filepath.Join(workspaceDir, path)); err != nil || string(data) != writer {
			t.Errorf("workspace file %s = %q, %v", path, data, err)
		}
		if data, err := os.ReadFile(filepath.Join(storeRepo.OverlayRoot(storeID), path)); err != nil || string(data) != writer {
			t.Errorf("store overlay %s = %q, %v", path, data, err)
		}
	}
}

func runConflictingHelpers(t *testing.T, root, operation string, release func() error) {
	t.Helper()
	commands := make([]*exec.Cmd, 0, 2)
	donePaths := make([]string, 0, 2)
	for _, writer := range []string{"writer-one", "writer-two"} {
		started := filepath.Join(root, operation+"-"+writer+"-started")
		done := filepath.Join(root, operation+"-"+writer+"-done")
		cmd := concurrencyHelperCommand(root, operation, writer, started, done)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
		donePaths = append(donePaths, done)
		waitForPath(t, started)
	}

	time.Sleep(100 * time.Millisecond)
	for _, done := range donePaths {
		if _, err := os.Stat(done); err == nil {
			t.Fatalf("%s completed while the conflicting resource lock was held", operation)
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("%s helper failed: %v", operation, err)
		}
	}
}

func concurrencyHelperCommand(root, operation, writer, started, done string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentWorkspaceAndStoreMutationsSerializeAcrossProcesses$")
	cmd.Env = append(os.Environ(),
		concurrencyHelperEnv+"=1",
		"MONODEV_CONCURRENCY_ROOT="+root,
		"MONODEV_CONCURRENCY_OPERATION="+operation,
		"MONODEV_CONCURRENCY_WRITER="+writer,
		"MONODEV_CONCURRENCY_STARTED="+started,
		"MONODEV_CONCURRENCY_DONE="+done,
	)
	return cmd
}

func runConcurrencyHelper(t *testing.T) {
	root := os.Getenv("MONODEV_CONCURRENCY_ROOT")
	operation := os.Getenv("MONODEV_CONCURRENCY_OPERATION")
	writer := os.Getenv("MONODEV_CONCURRENCY_WRITER")
	workspaceDir := filepath.Join(root, "workspace")
	fs := fsops.NewRealFS()
	paths := config.Paths{
		Root:       root,
		Stores:     filepath.Join(root, "stores"),
		Workspaces: filepath.Join(root, "workspaces"),
		Config:     filepath.Join(root, "config.yaml"),
	}
	eng := engine.New(
		gitx.NewFakeGitRepo(workspaceDir, "repo"),
		stores.NewFileStoreRepo(fs, paths.Stores),
		state.NewFileStateStore(fs, paths.Workspaces),
		fs,
		hash.NewSHA256Hasher(),
		&clock.RealClock{},
		paths,
	)
	if err := os.WriteFile(os.Getenv("MONODEV_CONCURRENCY_STARTED"), []byte("started"), 0600); err != nil {
		t.Fatal(err)
	}

	path := writer + ".txt"
	switch operation {
	case "track":
		_, err := eng.Track(context.Background(), &engine.TrackRequest{
			CWD:         workspaceDir,
			Paths:       []string{path},
			Description: writer,
		})
		if err != nil {
			t.Fatal(err)
		}
	case "commit":
		_, err := eng.Commit(context.Background(), &engine.CommitRequest{
			CWD:   workspaceDir,
			Paths: []string{path},
		})
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown helper operation %q", operation)
	}
	if err := os.WriteFile(os.Getenv("MONODEV_CONCURRENCY_DONE"), []byte("done"), 0600); err != nil {
		t.Fatal(err)
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func trackContains(track *stores.TrackFile, path string) bool {
	for _, tracked := range track.Tracked {
		if tracked.Path == path {
			return true
		}
	}
	return false
}
