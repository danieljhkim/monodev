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

// TestTrackedPath_LocationFieldExists verifies Location field exists and can be set.
// The actual population of Location is tested in integration tests.
func TestTrackedPath_LocationFieldExists(t *testing.T) {
	tp := stores.TrackedPath{
		Path:     "test.txt",
		Kind:     "file",
		Location: "/workspace/path",
	}

	if tp.Location != "/workspace/path" {
		t.Errorf("TrackedPath.Location = %s, want '/workspace/path'", tp.Location)
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
