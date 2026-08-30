package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ssh scp form",
			in:   "git@github.com:org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "https form",
			in:   "https://github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "https without git suffix",
			in:   "https://github.com/org/repo",
			want: "github.com/org/repo",
		},
		{
			name: "git protocol",
			in:   "git://github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "ssh scheme",
			in:   "ssh://git@github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "ssh scheme with port",
			in:   "ssh://git@github.com:22/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "non-default ssh port preserved",
			in:   "ssh://git@github.com:2222/org/repo.git",
			want: "github.com:2222/org/repo",
		},
		{
			name: "https with credentials",
			in:   "https://user:token@github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "https with user only",
			in:   "https://user@github.com/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "trailing slash",
			in:   "https://github.com/org/repo.git/",
			want: "github.com/org/repo",
		},
		{
			name: "double trailing slash",
			in:   "https://github.com/org/repo.git//",
			want: "github.com/org/repo",
		},
		{
			name: "uppercase host",
			in:   "https://GitHub.COM/org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "scp form uppercase host",
			in:   "git@GitHub.COM:org/repo.git",
			want: "github.com/org/repo",
		},
		{
			name: "path case preserved",
			in:   "https://github.com/Org/Repo.git",
			want: "github.com/Org/Repo",
		},
		{
			name: "query and fragment stripped",
			in:   "https://github.com/org/repo.git?foo=bar#frag",
			want: "github.com/org/repo",
		},
		{
			name: "local absolute path",
			in:   "/var/repos/org/repo.git",
			want: "/var/repos/org/repo",
		},
		{
			name: "local absolute path trailing slash",
			in:   "/var/repos/org/repo.git/",
			want: "/var/repos/org/repo",
		},
		{
			name: "file url",
			in:   "file:///var/repos/org/repo.git",
			want: "/var/repos/org/repo",
		},
		{
			name: "relative local path",
			in:   "../other-repo.git",
			want: "../other-repo",
		},
		{
			name: "dot local path",
			in:   "./repo.git",
			want: "repo",
		},
		{
			name: "whitespace trimmed",
			in:   "  git@github.com:org/repo.git  ",
			want: "github.com/org/repo",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "gitlab ssh",
			in:   "git@gitlab.com:group/sub/repo.git",
			want: "gitlab.com/group/sub/repo",
		},
		{
			name: "https default port dropped",
			in:   "https://github.com:443/org/repo.git",
			want: "github.com/org/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRemoteURL(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeRemoteURL_SSHAndHTTPSEquivalent(t *testing.T) {
	forms := []string{
		"git@github.com:org/repo.git",
		"git@github.com:org/repo",
		"ssh://git@github.com/org/repo.git",
		"ssh://git@github.com/org/repo",
		"https://github.com/org/repo.git",
		"https://github.com/org/repo",
		"https://github.com/org/repo.git/",
		"https://user:pass@github.com/org/repo.git",
		"git://github.com/org/repo.git",
		"https://GitHub.com/org/repo.git",
	}
	want := "github.com/org/repo"
	for _, form := range forms {
		got := NormalizeRemoteURL(form)
		if got != want {
			t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", form, got, want)
		}
	}
}

func TestSameRemoteIdentity(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "identical", a: "git@github.com:org/repo.git", b: "git@github.com:org/repo.git", want: true},
		{name: "ssh and https", a: "git@github.com:org/repo.git", b: "https://github.com/org/repo", want: true},
		{name: "credentials and case", a: "https://user:token@GitHub.com/org/repo.git/", b: "ssh://github.com/org/repo", want: true},
		{name: "different repos", a: "git@github.com:org/repo.git", b: "git@github.com:org/other.git", want: false},
		{name: "different hosts", a: "git@github.com:org/repo.git", b: "git@gitlab.com/org/repo.git", want: false},
		{name: "empty left", a: "", b: "git@github.com:org/repo.git", want: false},
		{name: "empty right", a: "git@github.com:org/repo.git", b: "", want: false},
		{name: "both empty", a: "", b: "", want: false},
		{name: "unrelated absolute local paths", a: "/srv/git/one.git", b: "/srv/git/two.git", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameRemoteIdentity(tt.a, tt.b); got != tt.want {
				t.Errorf("SameRemoteIdentity(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSameRemoteIdentity_ResolvesLocalFilesystemAliases(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(realDir, "remote.git"), 0755); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(base, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	canonical := filepath.Join(realDir, "remote.git")
	for _, alias := range []string{
		filepath.Join(aliasDir, "remote.git"),
		filepath.Join(aliasDir, "remote"),
		"file://" + filepath.Join(aliasDir, "remote.git"),
	} {
		if NormalizeRemoteURL(canonical) == NormalizeRemoteURL(alias) {
			t.Fatalf("alias %q already normalizes identically; the test no longer exercises alias resolution", alias)
		}
		if !SameRemoteIdentity(canonical, alias) {
			t.Errorf("SameRemoteIdentity(%q, %q) = false, want true", canonical, alias)
		}
	}

	// A sibling under the same aliased parent is a different repository.
	if SameRemoteIdentity(canonical, filepath.Join(aliasDir, "other.git")) {
		t.Error("distinct repositories under an aliased parent must not match")
	}
}

func TestSameRemoteIdentity_ResolvesAliasedParentOfMissingRepository(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(base, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Neither leaf exists on disk, matching a remote that lives on another
	// machine; the aliased parent still resolves.
	if !SameRemoteIdentity(filepath.Join(realDir, "gone.git"), filepath.Join(aliasDir, "gone.git")) {
		t.Error("aliased parent of a missing repository should still match")
	}
	if SameRemoteIdentity(filepath.Join(realDir, "gone.git"), filepath.Join(aliasDir, "other.git")) {
		t.Error("different missing repositories must not match")
	}
}

func TestSameRemoteIdentity_IgnoresRelativeLocalRemotes(t *testing.T) {
	// Relative remotes depend on the working directory, so they are only
	// equivalent when they normalize identically.
	if !SameRemoteIdentity("./remote.git", "./remote") {
		t.Error("identical relative remotes should match")
	}
	if SameRemoteIdentity("./remote.git", "../remote.git") {
		t.Error("differently spelled relative remotes must not be resolved against the working directory")
	}
}
