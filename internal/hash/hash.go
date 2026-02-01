// Package hash provides file hashing functionality for content comparison.
//
// Monodev uses SHA-256 hashes to detect changes in tracked files and determine
// when files have been modified (drift detection). The package provides both
// a real implementation using crypto/sha256 and a fake implementation for testing.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Hasher provides an abstraction for file hashing operations.
type Hasher interface {
	// HashFile computes the hash of the file at the given path.
	HashFile(path string) (string, error)
}

// SHA256Hasher implements Hasher using SHA-256.
type SHA256Hasher struct{}

// NewSHA256Hasher creates a new SHA256Hasher.
func NewSHA256Hasher() *SHA256Hasher {
	return &SHA256Hasher{}
}

// HashFile computes the SHA-256 hash of the file at the given path.
func (h *SHA256Hasher) HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	hashBytes := hasher.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}

// FakeHasher implements Hasher with deterministic hashes for testing.
type FakeHasher struct {
	hashes map[string]string
}

// NewFakeHasher creates a new FakeHasher.
func NewFakeHasher() *FakeHasher {
	return &FakeHasher{
		hashes: make(map[string]string),
	}
}

// SetHash sets the hash for a specific path (for testing).
func (h *FakeHasher) SetHash(path, hash string) {
	h.hashes[path] = hash
}

// HashFile returns the predetermined hash for the given path.
func (h *FakeHasher) HashFile(path string) (string, error) {
	if hash, ok := h.hashes[path]; ok {
		return hash, nil
	}
	// Default hash if not set
	return "fakehash", nil
}
