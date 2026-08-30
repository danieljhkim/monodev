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
	trimmed := raw
	if strings.HasPrefix(strings.ToLower(trimmed), "file://") {
		u, err := url.Parse(trimmed)
		if err == nil {
			if u.Path != "" {
				trimmed = u.Path
			} else {
				trimmed = strings.TrimPrefix(trimmed, "file://")
				trimmed = strings.TrimPrefix(trimmed, "file:")
			}
		}
	}
	cleaned := filepath.Clean(trimmed)
	cleaned = strings.ReplaceAll(cleaned, `\`, "/")
	return stripGitSuffix(strings.TrimRight(cleaned, "/"))
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
