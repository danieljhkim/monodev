package gitx

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	repoIDFileName      = "repo-id"
	gitMonodevDirName   = "monodev"
	monodevDirName      = ".monodev"
	legacyUnknownRemote = "unknown"

	fingerprintIDPrefix     = "id:"
	fingerprintRemotePrefix = "remote:"
	fingerprintPathPrefix   = "path:"
)

// HashFingerprint hashes identity material with SHA-256 hex encoding.
func HashFingerprint(material string) string {
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

// RemoteFingerprint is the current remote-based fingerprint for a raw git URL.
func RemoteFingerprint(rawURL string) string {
	return HashFingerprint(fingerprintRemotePrefix + NormalizeRemoteURL(rawURL))
}

// LegacyFingerprint is the pre-change fingerprint: SHA-256 of
// absRoot + "|" + rawRemoteURL. Missing remotes used the literal "unknown".
func LegacyFingerprint(absRoot, rawRemoteURL string) string {
	if rawRemoteURL == "" {
		rawRemoteURL = legacyUnknownRemote
	}
	return HashFingerprint(absRoot + "|" + rawRemoteURL)
}

// EnsureDurableRepoID returns the durable repository ID, creating one if needed.
//
// Preference:
//  1. repoRoot/.monodev/repo-id when the .monodev directory exists
//  2. <git-common-dir>/monodev/repo-id
//
// A newly generated ID is written to the git-common-dir location, and also to
// .monodev/repo-id when that directory already exists.
func EnsureDurableRepoID(root string) (string, error) {
	id, err := readDurableRepoID(root)
	if err != nil {
		return "", err
	}
	if id == "" {
		id, err = generateRepoID()
		if err != nil {
			return "", err
		}
	}
	if err := writeDurableRepoID(root, id); err != nil {
		return "", err
	}
	return id, nil
}

func readDurableRepoID(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	monodevIDPath := filepath.Join(absRoot, monodevDirName, repoIDFileName)
	if id, ok, err := readRepoIDFile(monodevIDPath); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}

	gitDir, err := NewRealGitRepo().CommonGitDir(absRoot)
	if err != nil {
		return "", nil
	}
	id, _, err := readRepoIDFile(filepath.Join(gitDir, gitMonodevDirName, repoIDFileName))
	if err != nil {
		return "", err
	}
	return id, nil
}

func writeDurableRepoID(root, id string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	gitDir, gitErr := NewRealGitRepo().CommonGitDir(absRoot)
	if gitErr == nil {
		if err := writeRepoIDFile(filepath.Join(gitDir, gitMonodevDirName, repoIDFileName), id); err != nil {
			return err
		}
	}

	monodevDir := filepath.Join(absRoot, monodevDirName)
	if info, err := os.Stat(monodevDir); err == nil && info.IsDir() {
		if err := writeRepoIDFile(filepath.Join(monodevDir, repoIDFileName), id); err != nil {
			return err
		}
	}
	if gitErr != nil {
		if _, statErr := os.Stat(monodevDir); statErr != nil {
			return fmt.Errorf("failed to persist durable repo id: %w", gitErr)
		}
	}
	return nil
}

func readRepoIDFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read repo id: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", false, nil
	}
	return id, true, nil
}

func writeRepoIDFile(path, id string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create repo id directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write repo id: %w", err)
	}
	return nil
}

func generateRepoID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate repo id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func selectRemoteURL(root string) (string, error) {
	names, err := gitRemoteNames(root)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", nil
	}

	chosen := names[0]
	for _, name := range names {
		if name == "origin" {
			chosen = name
			break
		}
	}

	cmd := execGitConfig(root, "remote."+chosen+".url")
	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(output)), nil
}

func gitRemoteNames(root string) ([]string, error) {
	cmd := execGit(root, "remote")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}
