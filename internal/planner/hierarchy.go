package planner

import (
	"path"
	"path/filepath"
	"strings"
)

// NormalizeTrackedPath returns a slash-separated cleaned relative path.
func NormalizeTrackedPath(rel string) string {
	cleaned := path.Clean(filepath.ToSlash(rel))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// IsStrictDescendant reports whether rel is nested strictly inside ancestor.
// Both paths are slash-separated workspace-relative paths.
func IsStrictDescendant(rel, ancestor string) bool {
	rel = NormalizeTrackedPath(rel)
	ancestor = NormalizeTrackedPath(ancestor)
	if rel == "" || ancestor == "" || rel == ancestor {
		return false
	}
	return strings.HasPrefix(rel, ancestor+"/")
}
