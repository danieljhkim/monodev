package engine

import (
	"bytes"
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
	entries, err := eng.managedExcludeEntries("packages/service", &state.WorkspaceState{Paths: map[string]state.PathOwnership{
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
