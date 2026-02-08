package engine

import (
	"testing"

	"github.com/danieljhkim/monodev/internal/stores"
)

// TestTrackRequest_HasCWDField verifies that TrackRequest has CWD field
// which is used to populate Location in tracked paths.
func TestTrackRequest_HasCWDField(t *testing.T) {
	req := &TrackRequest{
		CWD:   "/test/workspace",
		Paths: []string{"file.txt"},
	}

	if req.CWD != "/test/workspace" {
		t.Errorf("TrackRequest.CWD = %s, want '/test/workspace'", req.CWD)
	}
}

// TestTrackedPath_LocationFieldExists verifies the deprecated Location field still exists.
// As of schema v2, Location is unused; paths are repo-root-relative.
func TestTrackedPath_LocationFieldExists(t *testing.T) {
	tp := stores.TrackedPath{
		Path:     "test.txt",
		Kind:     "file",
		Location: "/workspace/path", //nolint:staticcheck // Testing deprecated field for backward compatibility
	}

	//nolint:staticcheck // Testing deprecated field for backward compatibility
	if tp.Location != "/workspace/path" {
		t.Errorf("TrackedPath.Location = %s, want '/workspace/path'", tp.Location) //nolint:staticcheck
	}
}

// TestTrackResult_ResolvedPaths verifies TrackResult structure.
func TestTrackResult_ResolvedPaths(t *testing.T) {
	result := &TrackResult{
		ResolvedPaths: map[string]string{
			"../../Makefile": "Makefile",
			"config.yaml":    "packages/web/config.yaml",
		},
	}

	if result.ResolvedPaths["../../Makefile"] != "Makefile" {
		t.Errorf("expected resolved path 'Makefile', got %q", result.ResolvedPaths["../../Makefile"])
	}
	if result.ResolvedPaths["config.yaml"] != "packages/web/config.yaml" {
		t.Errorf("expected resolved path 'packages/web/config.yaml', got %q", result.ResolvedPaths["config.yaml"])
	}
}

// TestUntrackRequest_HasCWDField verifies that UntrackRequest has CWD field.
func TestUntrackRequest_HasCWDField(t *testing.T) {
	req := &UntrackRequest{
		CWD:   "/test/workspace",
		Paths: []string{"file.txt"},
	}

	if req.CWD != "/test/workspace" {
		t.Errorf("UntrackRequest.CWD = %s, want '/test/workspace'", req.CWD)
	}
}
