package sync

import (
	"bytes"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	secretIgnoreFile = ".monodev-secretsignore"
	secretMask       = "********"
)

var (
	awsAccessKeyPattern = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	githubTokenPattern  = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36}\b`)
	openAIKeyPattern    = regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}\b`)
	anthropicKeyPattern = regexp.MustCompile(`\bsk-ant-api\d{2}-[A-Za-z0-9_-]{20,}\b`)
	privateKeyPattern   = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----`)
	assignmentPattern   = regexp.MustCompile(`(?i)\b[a-z][a-z0-9_-]*(?:secret|token|password|api[_-]?key)[a-z0-9_-]*\b\s*[:=]\s*(?:"([^"]+)"|'([^']+)'|([^\s#]+))`)
)

// SecretFinding reports a credential-like value without retaining the value.
// Path is relative to .monodev/persist/stores and always uses slash separators.
type SecretFinding struct {
	Path string
	Line int
	Rule string
}

func newSecretScanError(finding SecretFinding) error {
	return fmt.Errorf(
		"refusing to push plaintext persistence data: secret scan found %s:%d (rule %s, value %s); no commit was created. Remove or rotate it, add %q to the store's %s for a known false positive, or re-run with --allow-secrets. The persistence branch remains plaintext; scanning reduces accidental exposure but does not provide confidentiality",
		finding.Path,
		finding.Line,
		finding.Rule,
		secretMask,
		finding.Rule+":<relative-path>",
		secretIgnoreFile,
	)
}

// scanPersistedStores scans every materialized store that would be included in
// the persistence commit. It deliberately uses a compact in-repo ruleset so
// the static binary has no runtime dependency on an external scanner.
func scanPersistedStores(storesRoot string) (*SecretFinding, error) {
	entries, err := os.ReadDir(storesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		storeDir := filepath.Join(storesRoot, entry.Name())
		ignored, err := loadSecretIgnores(storeDir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Join(entry.Name(), secretIgnoreFile), err)
		}
		finding, err := scanStoreDirectory(storeDir, entry.Name(), ignored)
		if err != nil {
			return nil, err
		}
		if finding != nil {
			return finding, nil
		}
	}

	return nil, nil
}

func scanStoreDirectory(storeDir, storeID string, ignored secretIgnores) (*SecretFinding, error) {
	var finding *SecretFinding
	err := filepath.WalkDir(storeDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(storeDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == secretIgnoreFile {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isBinary(contents) {
			return nil
		}

		for lineNumber, line := range bytes.Split(contents, []byte("\n")) {
			rule := detectSecret(string(line))
			if rule == "" || ignored.matches(rule, rel) {
				continue
			}
			finding = &SecretFinding{
				Path: filepath.ToSlash(filepath.Join(storeID, rel)),
				Line: lineNumber + 1,
				Rule: rule,
			}
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return finding, nil
}

func isBinary(contents []byte) bool {
	probe := contents
	if len(probe) > 8*1024 {
		probe = probe[:8*1024]
	}
	return bytes.IndexByte(probe, 0) >= 0 || !utf8.Valid(probe)
}

func detectSecret(line string) string {
	switch {
	case nonPlaceholderMatch(awsAccessKeyPattern, line):
		return "aws-access-key"
	case nonPlaceholderMatch(githubTokenPattern, line):
		return "github-token"
	case nonPlaceholderMatch(anthropicKeyPattern, line):
		return "anthropic-api-key"
	case nonPlaceholderMatch(openAIKeyPattern, line):
		return "openai-api-key"
	case privateKeyPattern.MatchString(line):
		return "private-key-pem"
	}

	match := assignmentPattern.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	value := firstNonEmpty(match[1:])
	if isPlaceholder(value) || len(value) < 20 || shannonEntropy(value) < 3.5 {
		return ""
	}
	return "high-entropy-assignment"
}

func nonPlaceholderMatch(pattern *regexp.Regexp, line string) bool {
	match := pattern.FindString(line)
	return match != "" && !isPlaceholder(match)
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isPlaceholder(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "example") ||
		strings.Contains(lower, "placeholder") ||
		strings.Contains(lower, "changeme") ||
		strings.Contains(lower, "replace-me") ||
		strings.Contains(lower, "your_api_key")
}

func shannonEntropy(value string) float64 {
	counts := make(map[rune]int)
	for _, char := range value {
		counts[char]++
	}
	length := float64(len([]rune(value)))
	if length == 0 {
		return 0
	}

	var entropy float64
	for _, count := range counts {
		probability := float64(count) / length
		entropy -= probability * math.Log2(probability)
	}
	return entropy
}

type secretIgnores map[string]map[string]struct{}

func loadSecretIgnores(storeDir string) (secretIgnores, error) {
	contents, err := os.ReadFile(filepath.Join(storeDir, secretIgnoreFile))
	if err != nil {
		if os.IsNotExist(err) {
			return secretIgnores{}, nil
		}
		return nil, err
	}

	ignored := secretIgnores{}
	for lineNumber, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || parts[0] == "" || !validIgnorePath(parts[1]) {
			return nil, fmt.Errorf("invalid ignore entry on line %d; use rule-id:relative-path", lineNumber+1)
		}
		rule := parts[0]
		path := filepath.ToSlash(filepath.Clean(parts[1]))
		if ignored[rule] == nil {
			ignored[rule] = make(map[string]struct{})
		}
		ignored[rule][path] = struct{}{}
	}
	return ignored, nil
}

func validIgnorePath(path string) bool {
	clean := filepath.Clean(path)
	return path != "" && !filepath.IsAbs(path) && clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func (ignored secretIgnores) matches(rule, path string) bool {
	_, ok := ignored[rule][path]
	return ok
}
