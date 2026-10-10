package fsops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func writeSourceFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func sourceCopiers(fs *RealFS) map[string]func(string, string) error {
	return map[string]func(string, string) error{
		"Copy":           fs.Copy,
		"CopyExcept":     func(src, dst string) error { return fs.CopyExcept(src, dst, map[string]bool{"ignored": true}) },
		"CopyWithinRoot": func(src, dst string) error { return fs.CopyWithinRoot(filepath.Dir(dst), filepath.Base(dst), src) },
	}
}

func TestManagedCopiesRejectSourceLinksBeforeDestinationMutation(t *testing.T) {
	for method, copy := range sourceCopiers(NewRealFS()) {
		for _, location := range []string{"ancestor", "root", "descendant", "leaf"} {
			t.Run(method+"/"+location, func(t *testing.T) {
				base := t.TempDir()
				src := filepath.Join(base, "source", "tree")
				outside := filepath.Join(base, "outside")
				writeSourceFixture(t, filepath.Join(outside, "tree", "private.txt"), "outside-secret-sentinel")
				switch location {
				case "ancestor":
					requireSymlink(t, outside, filepath.Join(base, "source"))
				case "root":
					if err := os.MkdirAll(filepath.Dir(src), 0700); err != nil {
						t.Fatal(err)
					}
					requireSymlink(t, filepath.Join(outside, "tree"), src)
				case "descendant", "leaf":
					writeSourceFixture(t, filepath.Join(src, "safe.txt"), "safe")
					target := filepath.Join(outside, "tree")
					if location == "leaf" {
						target = filepath.Join(target, "private.txt")
					}
					requireSymlink(t, target, filepath.Join(src, "leak"))
				}
				dst := filepath.Join(base, "dst")
				writeSourceFixture(t, dst, "original")
				if err := copy(src, dst); err == nil || !strings.Contains(err.Error(), "symlink") {
					t.Fatalf("copy error = %v", err)
				}
				if got, err := os.ReadFile(dst); err != nil || string(got) != "original" {
					t.Fatalf("destination = %q, %v", got, err)
				}
				entries, err := os.ReadDir(base)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".monodev-") {
						t.Fatalf("stray staging: %s", entry.Name())
					}
				}
				if err := ValidateCopySource(src); err == nil {
					t.Fatal("source validator accepted link")
				}
			})
		}
	}
}

// Swap components at the exact preflight/execution boundary rather than relying
// on scheduler timing. These are the descriptor routines all three APIs use.
func TestManagedSourceReplacementAfterPreflight(t *testing.T) {
	for _, confined := range []bool{false, true} {
		for _, replaced := range []string{"ancestor", "root", "directory", "leaf"} {
			t.Run(replaced+map[bool]string{false: "/path", true: "/confined"}[confined], func(t *testing.T) {
				base := t.TempDir()
				src := filepath.Join(base, "ancestor", "source")
				writeSourceFixture(t, filepath.Join(src, "nested", "private.txt"), "safe")
				outside := filepath.Join(base, "outside")
				writeSourceFixture(t, filepath.Join(outside, "nested", "private.txt"), "outside-secret-sentinel")
				handle, err := openCopySource(src)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = handle.Close() }()
				if err := validateSourceHandle(handle, ".", nil); err != nil {
					t.Fatal(err)
				}
				path, target := src, outside
				switch replaced {
				case "ancestor":
					path = filepath.Dir(src)
				case "directory":
					path, target = filepath.Join(src, "nested"), filepath.Join(outside, "nested")
				case "leaf":
					path, target = filepath.Join(src, "nested", "private.txt"), filepath.Join(outside, "nested", "private.txt")
				}
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				requireSymlink(t, target, path)
				dst := filepath.Join(base, "destination")
				if confined {
					fd, openErr := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
					if openErr != nil {
						t.Fatal(openErr)
					}
					defer func() { _ = unix.Close(fd) }()
					err = NewRealFS().copyAt(handle, fd, "destination", ".")
				} else {
					if err := os.Mkdir(dst, 0700); err != nil {
						t.Fatal(err)
					}
					err = NewRealFS().copySourceContents(handle, dst, ".", map[string]bool{"ignored": true})
				}
				if replaced == "directory" || replaced == "leaf" {
					if err == nil || !strings.Contains(err.Error(), "symlink") {
						t.Fatalf("replacement accepted: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(dst, "nested", "private.txt"))
					if err != nil || string(got) != "safe" {
						t.Fatalf("pinned source copied %q, %v", got, err)
					}
				}
			})
		}
	}
}

func TestSourceAncestorHandleSurvivesReplacement(t *testing.T) {
	base := t.TempDir()
	ancestor := filepath.Join(base, "ancestor")
	writeSourceFixture(t, filepath.Join(ancestor, "private.txt"), "safe")
	outside := filepath.Join(base, "outside")
	writeSourceFixture(t, filepath.Join(outside, "private.txt"), "outside-secret-sentinel")
	handle, err := openCopySource(ancestor)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	if err := os.Rename(ancestor, ancestor+"-original"); err != nil {
		t.Fatal(err)
	}
	requireSymlink(t, outside, ancestor)
	file, err := openSourceAt(handle, "private.txt", "private.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	dst := filepath.Join(base, "dst")
	if err := writeFileAtomically(dst, file, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "safe" {
		t.Fatalf("copied %q, %v", got, err)
	}
	if _, err := OpenRegularSource(filepath.Join(ancestor, "private.txt")); err == nil {
		t.Fatal("fresh open followed replaced ancestor")
	}
}

func TestManagedCopiesSupportCanonicalPlatformRoots(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{base}
	if runtime.GOOS == "darwin" && strings.HasPrefix(base, "/private/var/") {
		roots = append(roots, strings.TrimPrefix(base, "/private"))
	}
	for _, root := range roots {
		for method, copy := range sourceCopiers(NewRealFS()) {
			t.Run(method+root, func(t *testing.T) {
				src := filepath.Join(root, "source")
				writeSourceFixture(t, filepath.Join(src, "nested", "file"), "safe")
				dst := filepath.Join(base, method)
				if err := copy(src, dst); err != nil {
					t.Fatal(err)
				}
				if got, err := os.ReadFile(filepath.Join(dst, "nested", "file")); err != nil || string(got) != "safe" {
					t.Fatalf("copied %q, %v", got, err)
				}
			})
		}
	}
}

func TestManagedCopiesNeverFollowConcurrentSourceReplacement(t *testing.T) {
	for method, copy := range sourceCopiers(NewRealFS()) {
		for _, replace := range []string{"ancestor", "leaf"} {
			t.Run(method+"/"+replace, func(t *testing.T) {
				base := t.TempDir()
				src := filepath.Join(base, "source")
				writeSourceFixture(t, filepath.Join(src, "private.txt"), "safe")
				outside := filepath.Join(base, "outside")
				writeSourceFixture(t, filepath.Join(outside, "private.txt"), "outside-secret-sentinel")
				path, target := src, outside
				if replace == "leaf" {
					path, target = filepath.Join(src, "private.txt"), filepath.Join(outside, "private.txt")
				}
				stop, done := make(chan struct{}), make(chan error, 1)
				go func() {
					for {
						select {
						case <-stop:
							done <- nil
							return
						default:
						}
						if err := os.Rename(path, path+"-original"); err != nil {
							done <- err
							return
						}
						if err := os.Symlink(target, path); err != nil {
							done <- err
							return
						}
						if err := os.Remove(path); err != nil {
							done <- err
							return
						}
						if err := os.Rename(path+"-original", path); err != nil {
							done <- err
							return
						}
					}
				}()
				defer func() {
					close(stop)
					if err := <-done; err != nil {
						t.Error(err)
					}
				}()
				dst := filepath.Join(base, "dst")
				for i := 0; i < 40; i++ {
					err := copy(src, dst)
					data, readErr := os.ReadFile(filepath.Join(dst, "private.txt"))
					if readErr == nil && string(data) != "safe" {
						t.Fatalf("outside target copied: %q (copy error %v)", data, err)
					}
					// Concurrent deletion may produce an empty snapshot. Inspect all
					// copied leaves, including temporarily renamed source entries.
					walkErr := filepath.WalkDir(dst, func(path string, entry os.DirEntry, walkErr error) error {
						if os.IsNotExist(walkErr) {
							return nil
						}
						if walkErr != nil {
							return walkErr
						}
						if entry.IsDir() {
							return nil
						}
						data, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						if strings.Contains(string(data), "outside-secret-sentinel") {
							t.Fatalf("outside target copied to %s", path)
						}
						return nil
					})
					if walkErr != nil {
						t.Fatal(walkErr)
					}
				}
			})
		}
	}
}
