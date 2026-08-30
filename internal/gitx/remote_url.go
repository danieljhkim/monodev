package gitx

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// scpLike matches git SCP-style remotes: [user@]host:path
	// where path does not start with '/' (that would be a URI or local path).
	scpLike      = regexp.MustCompile(`^(?:[^/@]+@)?([^:]+):([^/].+)$`)
	windowsDrive = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

// NormalizeRemoteURL returns a canonical form of a git remote URL so that
// equivalent checkouts hash to the same repository fingerprint.
//
// The normalizer:
//   - strips scheme, userinfo/credentials, query, and fragment
//   - lowercases the host
//   - drops default ports (22, 80, 443)
//   - strips a trailing ".git" suffix and trailing slashes
//   - treats git@host:org/repo and https://host/org/repo.git as identical
//
// Local paths and file:// URLs are cleaned and have ".git"/trailing slashes
// stripped, but are not host-lowercased.
func NormalizeRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if isLocalRemote(raw) {
		return normalizeLocalRemote(raw)
	}

	if m := scpLike.FindStringSubmatch(raw); m != nil {
		host := strings.ToLower(m[1])
		return joinHostPath(host, m[2])
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return stripGitSuffix(strings.Trim(raw, "/"))
	}

	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && !isDefaultPort(parsed.Scheme, port) {
		host = host + ":" + port
	}
	return joinHostPath(host, parsed.Path)
}

func isLocalRemote(raw string) bool {
	if windowsDrive.MatchString(raw) {
		return true
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		// UNC path, not an http URL (those have a scheme).
		if !strings.Contains(raw, "://") {
			return true
		}
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(raw), "file://") {
		return true
	}
	return false
}

func normalizeLocalRemote(raw string) string {
	cleaned := filepath.Clean(localRemotePath(raw))
	cleaned = strings.ReplaceAll(cleaned, `\`, "/")
	return stripGitSuffix(strings.TrimRight(cleaned, "/"))
}

// localRemotePath returns the filesystem path a local remote refers to,
// unwrapping a "file://" prefix. It performs no cleaning, so the result can
// still be resolved against the filesystem.
func localRemotePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), "file://") {
		return trimmed
	}
	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		return u.Path
	}
	trimmed = strings.TrimPrefix(trimmed, "file://")
	return strings.TrimPrefix(trimmed, "file:")
}

// SameRemoteIdentity reports whether two raw git remote URLs name the same
// repository.
//
// It is the comparison counterpart of NormalizeRemoteURL: two remotes match
// when they normalize identically, and additionally when both are absolute
// local paths that resolve to the same location on this filesystem. The
// second rule covers aliases such as macOS `/tmp` -> `/private/tmp`, or a
// symlinked checkout parent, which normalization alone cannot collapse
// because it never touches the filesystem.
//
// Filesystem resolution is deliberately kept out of NormalizeRemoteURL: that
// function feeds repository fingerprints, which must stay stable and
// reproducible without disk access. An empty identity is never equivalent to
// anything, so unknown remotes fail closed.
func SameRemoteIdentity(a, b string) bool {
	normalizedA, normalizedB := NormalizeRemoteURL(a), NormalizeRemoteURL(b)
	if normalizedA == "" || normalizedB == "" {
		return false
	}
	if normalizedA == normalizedB {
		return true
	}

	resolvedA, okA := resolveLocalRemoteIdentity(a)
	resolvedB, okB := resolveLocalRemoteIdentity(b)
	return okA && okB && resolvedA == resolvedB
}

// resolveLocalRemoteIdentity normalizes a local remote after resolving
// filesystem aliases. It reports false for anything that is not an absolute
// local path, including relative paths (whose meaning depends on the working
// directory) and every network remote.
func resolveLocalRemoteIdentity(raw string) (string, bool) {
	if !isLocalRemote(strings.TrimSpace(raw)) {
		return "", false
	}
	localPath := localRemotePath(raw)
	if !filepath.IsAbs(localPath) {
		return "", false
	}
	resolved, ok := evalSymlinksBestEffort(localPath)
	if !ok {
		return "", false
	}
	return NormalizeRemoteURL(resolved), true
}

// evalSymlinksBestEffort resolves symlinks in path, falling back to the
// deepest ancestor that exists and re-appending the missing components. A
// bare repository that is absent from this machine still benefits from an
// aliased parent such as `/tmp` being resolved.
func evalSymlinksBestEffort(localPath string) (string, bool) {
	dir, missing := filepath.Clean(localPath), ""
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, missing), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		missing = filepath.Join(filepath.Base(dir), missing)
		dir = parent
	}
}

func joinHostPath(host, rawPath string) string {
	p := path.Clean("/" + strings.TrimSpace(rawPath))
	p = strings.TrimPrefix(p, "/")
	p = stripGitSuffix(p)
	p = strings.Trim(p, "/")
	if host == "" {
		return p
	}
	if p == "" {
		return host
	}
	return host + "/" + p
}

func stripGitSuffix(s string) string {
	s = strings.TrimRight(s, "/")
	if strings.HasSuffix(strings.ToLower(s), ".git") {
		s = s[:len(s)-len(".git")]
	}
	return strings.TrimRight(s, "/")
}

func isDefaultPort(scheme, port string) bool {
	switch strings.ToLower(scheme) {
	case "http":
		return port == "80"
	case "https":
		return port == "443"
	case "ssh", "git":
		return port == "22"
	default:
		return port == "22" || port == "80" || port == "443"
	}
}
