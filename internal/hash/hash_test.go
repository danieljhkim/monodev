package hash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256Hasher_HashFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "hash-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hasher := NewSHA256Hasher()

	t.Run("hash of existing file", func(t *testing.T) {
		// Create a test file with known content
		testFile := filepath.Join(tmpDir, "test.txt")
		content := []byte("hello world")
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Hash the file
		hash1, err := hasher.HashFile(testFile)
		if err != nil {
			t.Fatalf("HashFile failed: %v", err)
		}

		// Verify hash is not empty
		if hash1 == "" {
			t.Error("HashFile returned empty hash")
		}

		// Hash the same file again - should get same result
		hash2, err := hasher.HashFile(testFile)
		if err != nil {
			t.Fatalf("HashFile failed on second call: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("HashFile inconsistent: got %s and %s", hash1, hash2)
		}
	})

	t.Run("different files have different hashes", func(t *testing.T) {
		// Create two files with different content
		file1 := filepath.Join(tmpDir, "file1.txt")
		file2 := filepath.Join(tmpDir, "file2.txt")

		if err := os.WriteFile(file1, []byte("content A"), 0644); err != nil {
			t.Fatalf("failed to write file1: %v", err)
		}
		if err := os.WriteFile(file2, []byte("content B"), 0644); err != nil {
			t.Fatalf("failed to write file2: %v", err)
		}

		hash1, err := hasher.HashFile(file1)
		if err != nil {
			t.Fatalf("HashFile failed for file1: %v", err)
		}

		hash2, err := hasher.HashFile(file2)
		if err != nil {
			t.Fatalf("HashFile failed for file2: %v", err)
		}

		if hash1 == hash2 {
			t.Error("Different files produced same hash")
		}
	})

	t.Run("same content produces same hash", func(t *testing.T) {
		// Create two files with identical content
		file1 := filepath.Join(tmpDir, "same1.txt")
		file2 := filepath.Join(tmpDir, "same2.txt")
		content := []byte("identical content")

		if err := os.WriteFile(file1, content, 0644); err != nil {
			t.Fatalf("failed to write file1: %v", err)
		}
		if err := os.WriteFile(file2, content, 0644); err != nil {
			t.Fatalf("failed to write file2: %v", err)
		}

		hash1, err := hasher.HashFile(file1)
		if err != nil {
			t.Fatalf("HashFile failed for file1: %v", err)
		}

		hash2, err := hasher.HashFile(file2)
		if err != nil {
			t.Fatalf("HashFile failed for file2: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("Identical content produced different hashes: %s vs %s", hash1, hash2)
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does-not-exist.txt")
		_, err := hasher.HashFile(nonExistent)
		if err == nil {
			t.Error("Expected error for non-existent file, got nil")
		}
	})

	t.Run("empty file can be hashed", func(t *testing.T) {
		emptyFile := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
			t.Fatalf("failed to write empty file: %v", err)
		}

		hash, err := hasher.HashFile(emptyFile)
		if err != nil {
			t.Fatalf("HashFile failed for empty file: %v", err)
		}

		// SHA-256 of empty string is a known value
		expectedEmptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		if hash != expectedEmptyHash {
			t.Errorf("Empty file hash incorrect: got %s, want %s", hash, expectedEmptyHash)
		}
	})
}

func TestFakeHasher(t *testing.T) {
	hasher := NewFakeHasher()

	t.Run("returns default hash for unknown path", func(t *testing.T) {
		hash, err := hasher.HashFile("/some/path")
		if err != nil {
			t.Errorf("FakeHasher should not return error, got: %v", err)
		}
		if hash != "fakehash" {
			t.Errorf("Expected default hash 'fakehash', got: %s", hash)
		}
	})

	t.Run("returns configured hash for known path", func(t *testing.T) {
		testPath := "/test/file.txt"
		expectedHash := "custom-hash-123"

		hasher.SetHash(testPath, expectedHash)

		hash, err := hasher.HashFile(testPath)
		if err != nil {
			t.Errorf("FakeHasher should not return error, got: %v", err)
		}
		if hash != expectedHash {
			t.Errorf("Expected hash %s, got: %s", expectedHash, hash)
		}
	})

	t.Run("multiple paths with different hashes", func(t *testing.T) {
		path1 := "/path/one"
		path2 := "/path/two"
		hash1 := "hash-one"
		hash2 := "hash-two"

		hasher.SetHash(path1, hash1)
		hasher.SetHash(path2, hash2)

		result1, _ := hasher.HashFile(path1)
		result2, _ := hasher.HashFile(path2)

		if result1 != hash1 {
			t.Errorf("Path1: expected %s, got %s", hash1, result1)
		}
		if result2 != hash2 {
			t.Errorf("Path2: expected %s, got %s", hash2, result2)
		}
	})
}
