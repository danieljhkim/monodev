package state

import (
	"crypto/sha256"
	"encoding/hex"
)

// ComputeWorkspaceID computes a stable workspace ID from the repository fingerprint
// and workspace path. This ID is used to uniquely identify workspace state files.
func ComputeWorkspaceID(repoFingerprint, workspacePath string) string {
	// Concatenate repo fingerprint and workspace path
	data := repoFingerprint + "|" + workspacePath

	// Compute SHA-256 hash
	hash := sha256.Sum256([]byte(data))

	// Return hex-encoded hash
	return hex.EncodeToString(hash[:])
}
