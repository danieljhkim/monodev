package stores

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/lockfile"
)

// setupStoresDir creates a temporary stores directory for testing.
func setupStoresDir(t *testing.T) (string, *FileStoreRepo) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "stores-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	fs := fsops.NewRealFS()
	repo := NewFileStoreRepo(fs, tmpDir)

	return tmpDir, repo
}

func TestFileStoreRepo_List(t *testing.T) {
	t.Run("does not expose the lock directory as a store", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		lock, err := repo.LockStore(context.Background(), "store1", lockfile.Exclusive)
		if err != nil {
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		ids, err := repo.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 0 {
			t.Fatalf("List() = %v, want no stores", ids)
		}
	})

	t.Run("returns empty list when directory does not exist", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "stores-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Use a non-existent subdirectory
		nonExistentDir := filepath.Join(tmpDir, "nonexistent")
		fs := fsops.NewRealFS()
		repo := NewFileStoreRepo(fs, nonExistentDir)

		stores, err := repo.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(stores) != 0 {
			t.Errorf("Expected empty list, got %d stores", len(stores))
		}
	})

	t.Run("returns empty list when directory is empty", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		stores, err := repo.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(stores) != 0 {
			t.Errorf("Expected empty list, got %d stores", len(stores))
		}
	})

	t.Run("returns list of store directories", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Create some store directories
		storeIDs := []string{"store1", "store2", "store3"}
		for _, id := range storeIDs {
			if err := os.MkdirAll(filepath.Join(tmpDir, id), 0755); err != nil {
				t.Fatalf("failed to create store dir: %v", err)
			}
		}

		// Create a regular file (should be ignored)
		if err := os.WriteFile(filepath.Join(tmpDir, "regular-file.txt"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create regular file: %v", err)
		}

		stores, err := repo.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(stores) != len(storeIDs) {
			t.Errorf("Expected %d stores, got %d", len(storeIDs), len(stores))
		}

		// Check all expected stores are present
		storeMap := make(map[string]bool)
		for _, id := range stores {
			storeMap[id] = true
		}

		for _, expectedID := range storeIDs {
			if !storeMap[expectedID] {
				t.Errorf("Expected store %q not found in list", expectedID)
			}
		}
	})
}

func TestFileStoreRepo_Exists(t *testing.T) {
	t.Run("returns false for non-existent store", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		exists, err := repo.Exists("nonexistent")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if exists {
			t.Error("Expected false for non-existent store")
		}
	})

	t.Run("returns true for existing store", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		if err := os.MkdirAll(filepath.Join(tmpDir, storeID), 0755); err != nil {
			t.Fatalf("failed to create store dir: %v", err)
		}

		exists, err := repo.Exists(storeID)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if !exists {
			t.Error("Expected true for existing store")
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_, err := repo.Exists("../invalid")
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

func TestFileStoreRepo_Create(t *testing.T) {
	t.Run("creates new store with metadata", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "new-store"
		now := time.Now()
		meta := NewStoreMeta("Test Store", now)
		meta.Description = "A test store"

		err := repo.Create(storeID, meta)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Verify store directory exists
		storePath := filepath.Join(tmpDir, storeID)
		if _, err := os.Stat(storePath); os.IsNotExist(err) {
			t.Error("Store directory was not created")
		}

		// Verify overlay directory exists
		overlayPath := filepath.Join(storePath, "overlay")
		if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
			t.Error("Overlay directory was not created")
		}

		for _, path := range []string{storePath, overlayPath} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("failed to stat %s: %v", path, err)
			}
			if got := info.Mode().Perm(); got != 0700 {
				t.Errorf("directory %s mode = %04o, want 0700", path, got)
			}
		}
		for _, name := range []string{"meta.json", "track.json"} {
			path := filepath.Join(storePath, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("failed to stat %s: %v", path, err)
			}
			if got := info.Mode().Perm(); got != 0600 {
				t.Errorf("file %s mode = %04o, want 0600", path, got)
			}
		}

		// Verify metadata file exists and is correct
		loadedMeta, err := repo.LoadMeta(storeID)
		if err != nil {
			t.Fatalf("Failed to load metadata: %v", err)
		}

		if loadedMeta.Name != meta.Name {
			t.Errorf("Meta.Name = %s, want %s", loadedMeta.Name, meta.Name)
		}

		if loadedMeta.Description != meta.Description {
			t.Errorf("Meta.Description = %s, want %s", loadedMeta.Description, meta.Description)
		}

		// Verify track file exists
		track, err := repo.LoadTrack(storeID)
		if err != nil {
			t.Fatalf("Failed to load track file: %v", err)
		}

		if len(track.Tracked) != 0 {
			t.Errorf("Expected empty track file, got %d tracked paths", len(track.Tracked))
		}
	})

	t.Run("returns error for existing store", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "existing-store"
		meta := NewStoreMeta("Test", time.Now())

		// Create store first time
		if err := repo.Create(storeID, meta); err != nil {
			t.Fatalf("First Create failed: %v", err)
		}

		// Try to create again
		err := repo.Create(storeID, meta)
		if err == nil {
			t.Error("Expected error when creating existing store, got nil")
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		meta := NewStoreMeta("Test", time.Now())
		err := repo.Create("../invalid", meta)
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

// createFailureFS delegates to the real filesystem, with failures either before
// or after the selected mutation so rollback must handle partial output too.
type createFailureFS struct {
	*fsops.RealFS
	failPath   string
	afterWrite bool
	failure    error
	cleanupErr error
}

func (fs *createFailureFS) Mkdir(path string, perm os.FileMode) error {
	if path == fs.failPath {
		return fs.failure
	}
	return fs.RealFS.Mkdir(path, perm)
}

func (fs *createFailureFS) MkdirAll(path string, perm os.FileMode) error {
	if path != fs.failPath {
		return fs.RealFS.MkdirAll(path, perm)
	}
	if fs.afterWrite {
		if err := fs.RealFS.MkdirAll(path, perm); err != nil {
			return err
		}
	}
	return fs.failure
}

func (fs *createFailureFS) AtomicWrite(path string, data []byte, perm os.FileMode) error {
	if path != fs.failPath {
		return fs.RealFS.AtomicWrite(path, data, perm)
	}
	if fs.afterWrite {
		if err := fs.RealFS.AtomicWrite(path, data, perm); err != nil {
			return err
		}
	}
	return fs.failure
}

func (fs *createFailureFS) RemoveAll(path string) error {
	if fs.cleanupErr != nil {
		return fs.cleanupErr
	}
	return fs.RealFS.RemoveAll(path)
}

func TestFileStoreRepo_CreateFailureAllowsRetry(t *testing.T) {
	for _, stage := range []string{"parent", "store", "overlay", "meta.json", "track.json"} {
		for _, afterWrite := range []bool{false, true} {
			// The exclusive Mkdir contract leaves no directory on error.
			if afterWrite && (stage == "parent" || stage == "store") {
				continue
			}
			name := stage + " before write"
			if afterWrite {
				name = stage + " after write"
			}
			t.Run(name, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "stores")
				storePath := filepath.Join(root, "new-store")
				failPath := filepath.Join(storePath, stage)
				switch stage {
				case "parent":
					failPath = root
				case "store":
					failPath = storePath
				}
				injected := errors.New("injected " + stage + " creation failure")
				fs := &createFailureFS{RealFS: fsops.NewRealFS(), failPath: failPath, afterWrite: afterWrite, failure: injected}
				repo := NewFileStoreRepo(fs, root)
				meta := NewStoreMeta("Retry", time.Now())
				if err := repo.Create("new-store", meta); !errors.Is(err, injected) {
					t.Fatalf("Create error = %v, want injected failure", err)
				}
				if exists, err := repo.Exists("new-store"); err != nil || exists {
					t.Fatalf("Exists after failure = %v, %v; want false, nil", exists, err)
				}
				if ids, err := repo.List(); err != nil || len(ids) != 0 {
					t.Fatalf("List after failure = %v, %v; want no stores", ids, err)
				}
				fs.failPath = ""
				if err := repo.Create("new-store", meta); err != nil {
					t.Fatalf("retry Create: %v", err)
				}
				if got, err := repo.LoadMeta("new-store"); err != nil || got.Name != meta.Name {
					t.Fatalf("retry metadata = %v, %v", got, err)
				}
				if track, err := repo.LoadTrack("new-store"); err != nil || len(track.Tracked) != 0 {
					t.Fatalf("retry track = %v, %v; want empty track", track, err)
				}
				for _, name := range []string{"", "overlay", "meta.json", "track.json"} {
					info, err := os.Stat(filepath.Join(storePath, name))
					if err != nil {
						t.Fatal(err)
					}
					want := os.FileMode(0600)
					if name == "" || name == "overlay" {
						want = 0700
						if !info.IsDir() {
							t.Fatalf("%q is not a directory", name)
						}
					}
					if got := info.Mode().Perm(); got != want {
						t.Errorf("%q mode = %04o, want %04o", name, got, want)
					}
				}
			})
		}
	}
}

func TestFileStoreRepo_CreateReportsRollbackFailure(t *testing.T) {
	root := t.TempDir()
	writeErr := errors.New("injected track failure")
	cleanupErr := errors.New("injected cleanup failure")
	fs := &createFailureFS{
		RealFS: fsops.NewRealFS(), failPath: filepath.Join(root, "new-store", "track.json"),
		failure: writeErr, cleanupErr: cleanupErr,
	}
	err := NewFileStoreRepo(fs, root).Create("new-store", NewStoreMeta("Test", time.Now()))
	if !errors.Is(err, writeErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Create error = %v, want both write and rollback errors", err)
	}
}

// staleCreateLookupFS forces both creators past the existence check before
// either can claim the directory, reproducing the TOCTOU race deterministically.
type staleCreateLookupFS struct {
	*fsops.RealFS
	lookups chan struct{}
	proceed chan struct{}
}

func (fs *staleCreateLookupFS) Exists(path string) (bool, error) {
	exists, err := fs.RealFS.Exists(path)
	fs.lookups <- struct{}{}
	<-fs.proceed
	return exists, err
}

func TestFileStoreRepo_ConcurrentCreatePreservesWinner(t *testing.T) {
	root := t.TempDir()
	fs := &staleCreateLookupFS{RealFS: fsops.NewRealFS(), lookups: make(chan struct{}, 2), proceed: make(chan struct{})}
	metas := []*StoreMeta{NewStoreMeta("First", time.Now()), NewStoreMeta("Second", time.Now())}
	errs := make([]error, len(metas))
	var wg sync.WaitGroup
	for i := range metas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = NewFileStoreRepo(fs, root).Create("shared", metas[i])
		}()
	}
	<-fs.lookups
	<-fs.lookups
	close(fs.proceed)
	wg.Wait()
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner != -1 {
				t.Fatal("both concurrent creates succeeded")
			}
			winner = i
		} else if !strings.Contains(err.Error(), "store already exists: shared") {
			t.Fatalf("Create error = %v, want duplicate store refusal", err)
		}
	}
	if winner == -1 {
		t.Fatalf("neither create succeeded: %v", errs)
	}
	repo := NewFileStoreRepo(fsops.NewRealFS(), root)
	if got, err := repo.LoadMeta("shared"); err != nil || got.Name != metas[winner].Name {
		t.Fatalf("winner metadata = %v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "shared", "track.json")); err != nil {
		t.Fatalf("winner track file: %v", err)
	}
}

func TestFileStoreRepo_DuplicateCreatePreservesContents(t *testing.T) {
	root := t.TempDir()
	repo := NewFileStoreRepo(fsops.NewRealFS(), root)
	if err := repo.Create("existing", NewStoreMeta("Original", time.Now())); err != nil {
		t.Fatal(err)
	}
	track := NewTrackFile()
	track.Tracked = []TrackedPath{{Path: "keep.txt", Kind: "file"}}
	if err := repo.SaveTrack("existing", track); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing", "overlay", "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	before := make(map[string][]byte)
	for _, name := range []string{"meta.json", "track.json", "overlay/keep.txt"} {
		data, err := os.ReadFile(filepath.Join(root, "existing", name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = data
	}
	// Stale lookup also exercises a pre-existing store encountered at Mkdir,
	// rather than only the early Exists check.
	for _, staleLookup := range []bool{false, true} {
		var fs fsops.FS = fsops.NewRealFS()
		if staleLookup {
			fs = &absentCreateLookupFS{RealFS: fsops.NewRealFS()}
		}
		err := NewFileStoreRepo(fs, root).Create("existing", NewStoreMeta("Replacement", time.Now()))
		if err == nil || !strings.Contains(err.Error(), "store already exists: existing") {
			t.Fatalf("duplicate Create = %v", err)
		}
		for name, want := range before {
			got, err := os.ReadFile(filepath.Join(root, "existing", name))
			if err != nil || string(got) != string(want) {
				t.Fatalf("duplicate changed %s: %q, %v", name, got, err)
			}
		}
	}
}

type absentCreateLookupFS struct{ *fsops.RealFS }

func (fs *absentCreateLookupFS) Exists(string) (bool, error) { return false, nil }

func TestFileStoreRepo_LoadMeta(t *testing.T) {
	t.Run("loads metadata correctly", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		now := time.Now()
		originalMeta := NewStoreMeta("My Store", now)
		originalMeta.Description = "Test description"

		// Create store
		if err := repo.Create(storeID, originalMeta); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Load metadata
		loadedMeta, err := repo.LoadMeta(storeID)
		if err != nil {
			t.Fatalf("LoadMeta failed: %v", err)
		}

		if loadedMeta.Name != originalMeta.Name {
			t.Errorf("Name = %s, want %s", loadedMeta.Name, originalMeta.Name)
		}

		if loadedMeta.Description != originalMeta.Description {
			t.Errorf("Description = %s, want %s", loadedMeta.Description, originalMeta.Description)
		}
	})

	t.Run("returns error for non-existent store", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_, err := repo.LoadMeta("nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent store, got nil")
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_, err := repo.LoadMeta("../invalid")
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})

	t.Run("loads pre-change meta.json and ignores retired metadata", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "legacy-store"
		storeDir := filepath.Join(tmpDir, storeID)
		if err := os.MkdirAll(storeDir, 0700); err != nil {
			t.Fatalf("mkdir store: %v", err)
		}
		fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_meta.json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		if err := os.WriteFile(filepath.Join(storeDir, "meta.json"), fixture, 0600); err != nil {
			t.Fatalf("write meta.json: %v", err)
		}

		meta, err := repo.LoadMeta(storeID)
		if err != nil {
			t.Fatalf("LoadMeta: %v", err)
		}
		if meta.Name != "legacy-store" {
			t.Errorf("Name = %s, want legacy-store", meta.Name)
		}
		if meta.Description != "written by the pre-removal release" {
			t.Errorf("Description = %s, want fixture description", meta.Description)
		}
		if meta.SchemaVersion != 2 {
			t.Errorf("SchemaVersion = %d, want 2", meta.SchemaVersion)
		}

		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("marshal loaded meta: %v", err)
		}
		jsonStr := string(data)
		for _, key := range []string{"owner", "taskId", "scope"} {
			if strings.Contains(jsonStr, `"`+key+`"`) {
				t.Errorf("rewritten meta.json should not contain %q: %s", key, jsonStr)
			}
		}
	})
}

func TestFileStoreRepo_LoadsLegacyTrackAndRejectsFutureSchemas(t *testing.T) {
	tmpDir, repo := setupStoresDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storeID := "legacy-track"
	storeDir := filepath.Join(tmpDir, storeID)
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_track.json"))
	if err != nil {
		t.Fatalf("read legacy track fixture: %v", err)
	}
	trackPath := filepath.Join(storeDir, "track.json")
	if err := os.WriteFile(trackPath, fixture, 0600); err != nil {
		t.Fatalf("write legacy track fixture: %v", err)
	}
	track, err := repo.LoadTrack(storeID)
	if err != nil {
		t.Fatalf("LoadTrack() legacy fixture: %v", err)
	}
	if track.SchemaVersion != trackFileSchemaVersion || len(track.Tracked) != 1 || track.Tracked[0].Path != "scripts/setup.sh" {
		t.Fatalf("LoadTrack() = %#v, want pre-change fixture", track)
	}

	for _, tt := range []struct {
		name string
		path string
		load func() error
	}{
		{
			name: "meta",
			path: filepath.Join(storeDir, "meta.json"),
			load: func() error {
				_, err := repo.LoadMeta(storeID)
				return err
			},
		},
		{
			name: "track",
			path: trackPath,
			load: func() error {
				_, err := repo.LoadTrack(storeID)
				return err
			},
		},
	} {
		t.Run("rejects future "+tt.name, func(t *testing.T) {
			if err := os.WriteFile(tt.path, []byte(`{"schemaVersion":3}`), 0600); err != nil {
				t.Fatal(err)
			}
			err := tt.load()
			if err == nil {
				t.Fatal("load error = nil, want future schema refusal")
			}
			for _, want := range []string{tt.path, "schemaVersion 3", "supported schemaVersion 2", "upgrade monodev"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("load error = %q, want %q", err, want)
				}
			}
		})
	}
}

func TestFileStoreRepo_SaveMeta(t *testing.T) {
	t.Run("saves metadata correctly", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		originalMeta := NewStoreMeta("Original", time.Now())

		// Create store
		if err := repo.Create(storeID, originalMeta); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Update metadata
		updatedMeta := NewStoreMeta("Updated", time.Now())
		updatedMeta.Description = "New description"

		if err := repo.SaveMeta(storeID, updatedMeta); err != nil {
			t.Fatalf("SaveMeta failed: %v", err)
		}

		// Load and verify
		loadedMeta, err := repo.LoadMeta(storeID)
		if err != nil {
			t.Fatalf("LoadMeta failed: %v", err)
		}

		if loadedMeta.Name != updatedMeta.Name {
			t.Errorf("Name = %s, want %s", loadedMeta.Name, updatedMeta.Name)
		}

		if loadedMeta.Description != updatedMeta.Description {
			t.Errorf("Description = %s, want %s", loadedMeta.Description, updatedMeta.Description)
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		meta := NewStoreMeta("Test", time.Now())
		err := repo.SaveMeta("../invalid", meta)
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

func TestFileStoreRepo_LoadTrack(t *testing.T) {
	t.Run("loads track file correctly", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		meta := NewStoreMeta("Test", time.Now())

		// Create store
		if err := repo.Create(storeID, meta); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Create track file with data
		track := NewTrackFile()
		track.Tracked = []TrackedPath{
			{Path: "src/main.go", Kind: "file"},
			{Path: "config/", Kind: "dir"},
		}
		track.Ignore = []string{"*.log", "tmp/"}
		track.Notes = "Test notes"

		if err := repo.SaveTrack(storeID, track); err != nil {
			t.Fatalf("SaveTrack failed: %v", err)
		}

		// Load and verify
		loadedTrack, err := repo.LoadTrack(storeID)
		if err != nil {
			t.Fatalf("LoadTrack failed: %v", err)
		}

		if len(loadedTrack.Tracked) != len(track.Tracked) {
			t.Errorf("Tracked count = %d, want %d", len(loadedTrack.Tracked), len(track.Tracked))
		}

		if len(loadedTrack.Ignore) != len(track.Ignore) {
			t.Errorf("Ignore count = %d, want %d", len(loadedTrack.Ignore), len(track.Ignore))
		}

		if loadedTrack.Notes != track.Notes {
			t.Errorf("Notes = %s, want %s", loadedTrack.Notes, track.Notes)
		}
	})

	t.Run("returns empty track file if not exists", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		meta := NewStoreMeta("Test", time.Now())

		// Create store
		if err := repo.Create(storeID, meta); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Delete track file
		trackPath := filepath.Join(tmpDir, storeID, "track.json")
		if err := os.Remove(trackPath); err != nil {
			t.Fatalf("Failed to remove track file: %v", err)
		}

		// Load should return empty track file
		track, err := repo.LoadTrack(storeID)
		if err != nil {
			t.Fatalf("LoadTrack failed: %v", err)
		}

		if len(track.Tracked) != 0 {
			t.Errorf("Expected empty track file, got %d tracked paths", len(track.Tracked))
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_, err := repo.LoadTrack("../invalid")
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

func TestFileStoreRepo_SaveTrack(t *testing.T) {
	t.Run("saves track file correctly", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		meta := NewStoreMeta("Test", time.Now())

		// Create store
		if err := repo.Create(storeID, meta); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Create and save track file
		track := NewTrackFile()
		required := false
		track.Tracked = []TrackedPath{
			{Path: "file1.txt", Kind: "file"},
			{Path: "dir/", Kind: "dir", Required: &required},
		}

		if err := repo.SaveTrack(storeID, track); err != nil {
			t.Fatalf("SaveTrack failed: %v", err)
		}

		// Load and verify
		loadedTrack, err := repo.LoadTrack(storeID)
		if err != nil {
			t.Fatalf("LoadTrack failed: %v", err)
		}

		if len(loadedTrack.Tracked) != len(track.Tracked) {
			t.Errorf("Tracked count = %d, want %d", len(loadedTrack.Tracked), len(track.Tracked))
		}

		if loadedTrack.Tracked[0].Path != track.Tracked[0].Path {
			t.Errorf("First path = %s, want %s", loadedTrack.Tracked[0].Path, track.Tracked[0].Path)
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		track := NewTrackFile()
		err := repo.SaveTrack("../invalid", track)
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

func TestFileStoreRepo_OverlayRoot(t *testing.T) {
	t.Run("returns correct overlay path", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		overlayPath := repo.OverlayRoot(storeID)

		expectedPath := filepath.Join(tmpDir, storeID, "overlay")
		if overlayPath != expectedPath {
			t.Errorf("OverlayRoot = %s, want %s", overlayPath, expectedPath)
		}
	})

	t.Run("returns empty string for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		overlayPath := repo.OverlayRoot("../invalid")
		if overlayPath != "" {
			t.Errorf("Expected empty string for invalid ID, got %s", overlayPath)
		}
	})
}

func TestFileStoreRepo_Delete(t *testing.T) {
	t.Run("deletes store successfully", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		storeID := "test-store"
		meta := NewStoreMeta("Test", time.Now())

		// Create store
		if err := repo.Create(storeID, meta); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Verify it exists
		exists, err := repo.Exists(storeID)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Fatal("Store should exist before deletion")
		}

		// Delete store
		if err := repo.Delete(storeID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify it no longer exists
		exists, err = repo.Exists(storeID)
		if err != nil {
			t.Fatalf("Exists check after delete failed: %v", err)
		}
		if exists {
			t.Error("Store should not exist after deletion")
		}
	})

	t.Run("handles deletion of non-existent store", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Deleting non-existent store should not error (idempotent)
		err := repo.Delete("nonexistent")
		if err != nil {
			t.Errorf("Delete of non-existent store should not error, got: %v", err)
		}
	})

	t.Run("returns error for invalid store ID", func(t *testing.T) {
		tmpDir, repo := setupStoresDir(t)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		err := repo.Delete("../invalid")
		if err == nil {
			t.Error("Expected error for invalid store ID, got nil")
		}
	})
}

func TestFileStoreRepo_RefusesReservedCoordinationID(t *testing.T) {
	tmpDir, repo := setupStoresDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	locksDir := filepath.Join(tmpDir, ".locks")
	sentinel := filepath.Join(locksDir, "victim.lock")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(sentinel)
	if err != nil {
		t.Fatal(err)
	}

	meta := NewStoreMeta("nope", time.Now())
	track := NewTrackFile()
	assertReserved := func(err error) {
		t.Helper()
		if !errors.Is(err, ErrReservedStoreID) {
			t.Fatalf("error = %v, want ErrReservedStoreID", err)
		}
	}

	for _, id := range []string{".locks", ".LOCKS", ".Locks"} {
		_, err = repo.Exists(id)
		assertReserved(err)
		assertReserved(repo.Create(id, meta))
		_, err = repo.LoadMeta(id)
		assertReserved(err)
		assertReserved(repo.SaveMeta(id, meta))
		_, err = repo.LoadTrack(id)
		assertReserved(err)
		assertReserved(repo.SaveTrack(id, track))
		if got := repo.OverlayRoot(id); got != "" {
			t.Fatalf("OverlayRoot(%q) = %q, want empty", id, got)
		}
		assertReserved(repo.Delete(id))
		_, err = repo.StoreLockKey(id)
		assertReserved(err)
		_, err = repo.LockStore(context.Background(), id, lockfile.Exclusive)
		assertReserved(err)
	}

	ids, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if ReservedStoreID(id) {
			t.Fatalf("List exposed coordination directory %q", id)
		}
	}

	after, err := os.Stat(sentinel)
	if err != nil {
		t.Fatalf("coordination sentinel missing: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("coordination sentinel inode changed")
	}
	if _, err := os.Stat(filepath.Join(locksDir, "meta.json")); !os.IsNotExist(err) {
		t.Fatalf("reserved operation wrote meta.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(locksDir, "overlay")); !os.IsNotExist(err) {
		t.Fatalf("reserved operation created overlay: %v", err)
	}
}

func TestFileStoreRepo_ReservedIDDoesNotCreateLockDirectory(t *testing.T) {
	tmpDir, repo := setupStoresDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := repo.Delete(".locks"); !errors.Is(err, ErrReservedStoreID) {
		t.Fatalf("Delete error = %v, want ErrReservedStoreID", err)
	}
	if _, err := repo.LockStore(context.Background(), ".locks", lockfile.Exclusive); !errors.Is(err, ErrReservedStoreID) {
		t.Fatalf("LockStore error = %v, want ErrReservedStoreID", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".locks")); !os.IsNotExist(err) {
		t.Fatalf("reserved operation created .locks: %v", err)
	}
}

func TestDeleteReservedLockDirKeepsExclusiveExclusion(t *testing.T) {
	tmpDir, repo := setupStoresDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()
	held, err := repo.LockStore(ctx, "victim", lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	lockPath, err := repo.StoreLockKey("victim")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(".locks"); !errors.Is(err, ErrReservedStoreID) {
		t.Fatalf("Delete(.locks) error = %v, want ErrReservedStoreID", err)
	}

	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("lock file missing after refused delete: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("lock inode changed; a later acquire could succeed against a new inode")
	}

	started := time.Now()
	_, err = repo.LockStore(ctx, "victim", lockfile.Exclusive)
	elapsed := time.Since(started)
	if !errors.Is(err, lockfile.ErrContended) {
		t.Fatalf("second exclusive lock error = %v, want ErrContended", err)
	}
	if elapsed < lockfile.DefaultTimeout/2 || elapsed > lockfile.DefaultTimeout+2*time.Second {
		t.Fatalf("contention elapsed = %s, want a bounded wait near %s", elapsed, lockfile.DefaultTimeout)
	}
}

func TestOrdinaryDotPrefixedStoreID(t *testing.T) {
	tmpDir, repo := setupStoresDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	const id = ".editor"
	if err := repo.Create(id, NewStoreMeta("Editor", time.Now())); err != nil {
		t.Fatal(err)
	}
	exists, err := repo.Exists(id)
	if err != nil || !exists {
		t.Fatalf("Exists(%q) = %v, %v, want true, nil", id, exists, err)
	}
	ids, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, listed := range ids {
		if listed == id {
			found = true
		}
		if ReservedStoreID(listed) {
			t.Fatalf("List exposed %q", listed)
		}
	}
	if !found {
		t.Fatalf("List() = %v, want %s", ids, id)
	}
	if got := repo.OverlayRoot(id); got != filepath.Join(tmpDir, id, "overlay") {
		t.Fatalf("OverlayRoot(%q) = %q", id, got)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	exists, err = repo.Exists(id)
	if err != nil || exists {
		t.Fatalf("Exists after delete = %v, %v, want false, nil", exists, err)
	}
}

func TestWorkspaceIdentifierValidationStillAcceptsLockName(t *testing.T) {
	fs := fsops.NewRealFS()
	if err := fs.ValidateIdentifier(".locks"); err != nil {
		t.Fatalf("ValidateIdentifier(%q) = %v, want nil so workspace IDs stay compatible", ".locks", err)
	}
	if err := fs.ValidateIdentifier(".."); err == nil {
		t.Fatal("ValidateIdentifier(..) succeeded, want traversal rejection")
	}
}
