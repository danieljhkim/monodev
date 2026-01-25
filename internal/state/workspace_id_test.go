package state

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestComputeWorkspaceID_Basic(t *testing.T) {
	repoFingerprint := "abc123def456"
	workspacePath := "services/search/indexer"

	id := ComputeWorkspaceID(repoFingerprint, workspacePath)

	// ID should be a hex-encoded SHA-256 hash (64 characters)
	if len(id) != 64 {
		t.Errorf("expected ID length 64, got %d", len(id))
	}

	// Verify it's valid hex
	_, err := hex.DecodeString(id)
	if err != nil {
		t.Errorf("ID is not valid hex: %v", err)
	}
}

func TestComputeWorkspaceID_DifferentFingerprints(t *testing.T) {
	workspacePath := "services/search/indexer"

	fingerprints := []string{
		"abc123def456",
		"xyz789ghi012",
		"verylongfingerprint123456789012345678901234567890",
		"short",
		"",
	}

	ids := make(map[string]string)
	for _, fp := range fingerprints {
		id := ComputeWorkspaceID(fp, workspacePath)
		ids[fp] = id

		// Each fingerprint should produce a different ID
		for otherFP, otherID := range ids {
			if otherFP != fp && otherID == id {
				t.Errorf("different fingerprints produced same ID: %q and %q both produced %q", otherFP, fp, id)
			}
		}
	}
}

func TestComputeWorkspaceID_DifferentPaths(t *testing.T) {
	repoFingerprint := "abc123def456"

	paths := []string{
		"services/search/indexer",
		"services/search",
		"services",
		"",
		"very/long/path/to/workspace/with/many/components",
	}

	ids := make(map[string]string)
	for _, path := range paths {
		id := ComputeWorkspaceID(repoFingerprint, path)
		ids[path] = id

		// Each path should produce a different ID
		for otherPath, otherID := range ids {
			if otherPath != path && otherID == id {
				t.Errorf("different paths produced same ID: %q and %q both produced %q", otherPath, path, id)
			}
		}
	}
}

func TestComputeWorkspaceID_PathNormalization(t *testing.T) {
	repoFingerprint := "abc123def456"

	// Test that equivalent paths produce the same ID
	// Note: This test assumes the function doesn't normalize paths internally
	// If normalization is added, these tests should be updated

	tests := []struct {
		name  string
		path1 string
		path2 string
		same  bool
	}{
		{
			name:  "identical paths",
			path1: "services/search",
			path2: "services/search",
			same:  true,
		},
		{
			name:  "different paths",
			path1: "services/search",
			path2: "services/indexer",
			same:  false,
		},
		{
			name:  "empty vs non-empty",
			path1: "",
			path2: "services",
			same:  false,
		},
		{
			name:  "both empty",
			path1: "",
			path2: "",
			same:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := ComputeWorkspaceID(repoFingerprint, tt.path1)
			id2 := ComputeWorkspaceID(repoFingerprint, tt.path2)

			if tt.same && id1 != id2 {
				t.Errorf("expected same ID for equivalent paths, got %q and %q", id1, id2)
			}
			if !tt.same && id1 == id2 {
				t.Errorf("expected different IDs for different paths, both got %q", id1)
			}
		})
	}
}

func TestComputeWorkspaceID_Stability(t *testing.T) {
	// Test that the same inputs always produce the same output
	repoFingerprint := "abc123def456"
	workspacePath := "services/search/indexer"

	// Compute multiple times
	iterations := 100
	firstID := ComputeWorkspaceID(repoFingerprint, workspacePath)

	for i := 0; i < iterations; i++ {
		id := ComputeWorkspaceID(repoFingerprint, workspacePath)
		if id != firstID {
			t.Errorf("iteration %d: expected stable ID %q, got %q", i, firstID, id)
		}
	}
}

func TestComputeWorkspaceID_HashCorrectness(t *testing.T) {
	// Verify that the hash computation matches expected SHA-256 behavior
	repoFingerprint := "test-repo"
	workspacePath := "test-path"

	// Manual computation
	data := repoFingerprint + "|" + workspacePath
	expectedHash := sha256.Sum256([]byte(data))
	expectedID := hex.EncodeToString(expectedHash[:])

	// Function computation
	actualID := ComputeWorkspaceID(repoFingerprint, workspacePath)

	if actualID != expectedID {
		t.Errorf("ID mismatch: expected %q, got %q", expectedID, actualID)
	}
}

func TestComputeWorkspaceID_SeparatorHandling(t *testing.T) {
	// Test that the separator "|" is handled correctly
	repoFingerprint := "repo|with|pipes"
	workspacePath := "path|with|pipes"

	id1 := ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Different separator usage should produce different IDs
	repoFingerprint2 := "repo"
	workspacePath2 := "with|pipes|path"

	id2 := ComputeWorkspaceID(repoFingerprint2, workspacePath2)

	if id1 == id2 {
		t.Error("expected different IDs when separator appears in different positions")
	}
}

func TestComputeWorkspaceID_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		fingerprint   string
		workspacePath string
	}{
		{
			name:          "both empty",
			fingerprint:   "",
			workspacePath: "",
		},
		{
			name:          "empty fingerprint",
			fingerprint:   "",
			workspacePath: "services/search",
		},
		{
			name:          "empty path",
			fingerprint:   "abc123",
			workspacePath: "",
		},
		{
			name:          "very long fingerprint",
			fingerprint:   string(make([]byte, 1000)),
			workspacePath: "path",
		},
		{
			name:          "very long path",
			fingerprint:   "abc123",
			workspacePath: string(make([]byte, 1000)),
		},
		{
			name:          "unicode characters",
			fingerprint:   "repo-测试",
			workspacePath: "path-测试",
		},
		{
			name:          "special characters",
			fingerprint:   "repo!@#$%^&*()",
			workspacePath: "path!@#$%^&*()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := ComputeWorkspaceID(tt.fingerprint, tt.workspacePath)

			// Should always produce a valid 64-character hex string
			if len(id) != 64 {
				t.Errorf("expected ID length 64, got %d", len(id))
			}

			_, err := hex.DecodeString(id)
			if err != nil {
				t.Errorf("ID is not valid hex: %v", err)
			}

			// Should be stable
			id2 := ComputeWorkspaceID(tt.fingerprint, tt.workspacePath)
			if id != id2 {
				t.Errorf("ID not stable: got %q and %q", id, id2)
			}
		})
	}
}

func TestComputeWorkspaceID_CollisionResistance(t *testing.T) {
	// Test that different inputs produce different IDs (collision resistance)
	// This is a probabilistic test - SHA-256 should have very low collision probability

	repoFingerprint := "test-repo"
	paths := []string{
		"path1", "path2", "path3", "path4", "path5",
		"a", "b", "c", "d", "e",
		"very/long/path/with/many/components",
		"short",
		"",
	}

	ids := make(map[string]bool)
	for _, path := range paths {
		id := ComputeWorkspaceID(repoFingerprint, path)
		if ids[id] {
			t.Errorf("collision detected: path %q produced ID %q that was already seen", path, id)
		}
		ids[id] = true
	}

	// All IDs should be unique
	if len(ids) != len(paths) {
		t.Errorf("expected %d unique IDs, got %d", len(paths), len(ids))
	}
}

func TestComputeWorkspaceID_OrderMatters(t *testing.T) {
	// Test that swapping fingerprint and path produces different IDs
	fingerprint1 := "repo1"
	path1 := "path1"

	fingerprint2 := "path1"
	path2 := "repo1"

	id1 := ComputeWorkspaceID(fingerprint1, path1)
	id2 := ComputeWorkspaceID(fingerprint2, path2)

	if id1 == id2 {
		t.Error("expected different IDs when fingerprint and path are swapped")
	}
}
