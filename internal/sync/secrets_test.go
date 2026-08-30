package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const syntheticAWSAccessKey = "AKIA1234567890ABCDEF"

func TestScanPersistedStoresDetectsSecretsWithoutRetainingValues(t *testing.T) {
	storesRoot := t.TempDir()
	writeScanFixture(t, storesRoot, "demo", "overlay/.env", "AWS_ACCESS_KEY_ID="+syntheticAWSAccessKey+"\n")

	finding, err := scanPersistedStores(storesRoot)
	if err != nil {
		t.Fatalf("scanPersistedStores() error = %v", err)
	}
	if finding == nil {
		t.Fatal("scanPersistedStores() finding = nil, want AWS access key")
	}
	if finding.Path != "demo/overlay/.env" || finding.Line != 1 || finding.Rule != "aws-access-key" {
		t.Fatalf("finding = %#v, want demo/overlay/.env:1 aws-access-key", finding)
	}

	err = newSecretScanError(*finding)
	if got := err.Error(); !strings.Contains(got, secretMask) || strings.Contains(got, syntheticAWSAccessKey) {
		t.Fatalf("secret scan error must redact the credential, got %q", got)
	}
}

func TestDetectSecretHighConfidenceRules(t *testing.T) {
	githubToken := "ghp_" + strings.Repeat("a", 36)
	openAIKey := "sk-proj-" + strings.Repeat("a", 24)
	anthropicKey := "sk-ant-api03-" + strings.Repeat("a", 24)

	for _, testCase := range []struct {
		line string
		want string
	}{
		{line: "AWS_ACCESS_KEY_ID=" + syntheticAWSAccessKey, want: "aws-access-key"},
		{line: "GITHUB_TOKEN=" + githubToken, want: "github-token"},
		{line: "OPENAI_API_KEY=" + openAIKey, want: "openai-api-key"},
		{line: "ANTHROPIC_API_KEY=" + anthropicKey, want: "anthropic-api-key"},
		{line: "-----BEGIN PRIVATE KEY-----", want: "private-key-pem"},
		{line: "service_secret = q7V!9mK2xR4pL8dN3wZ6cB1h", want: "high-entropy-assignment"},
	} {
		t.Run(testCase.want, func(t *testing.T) {
			if got := detectSecret(testCase.line); got != testCase.want {
				t.Fatalf("detectSecret(%q) = %q, want %q", testCase.line, got, testCase.want)
			}
		})
	}
}

func TestScanPersistedStoresSkipsBinariesAndFalsePositiveFixtureCorpus(t *testing.T) {
	storesRoot := t.TempDir()
	writeScanFixture(t, storesRoot, "demo", "overlay/main.go", "package demo\n\nconst encodedFixture = \"QWxhZGRpbjpvcGVuIHNlc2FtZQ==\"\n")
	writeScanFixture(t, storesRoot, "demo", "overlay/Makefile", "API_KEY = example-placeholder-token\n")
	writeScanFixture(t, storesRoot, "demo", "overlay/README.md", "Use ghp_example_placeholder_token in documentation only.\n")
	writeScanFixture(t, storesRoot, "demo", "overlay/examples.md", "OPENAI_API_KEY=sk-proj-example-placeholder-value\n")
	writeScanFixture(t, storesRoot, "demo", "overlay/data.bin", append([]byte{0, 1, 2}, []byte(syntheticAWSAccessKey)...))

	finding, err := scanPersistedStores(storesRoot)
	if err != nil {
		t.Fatalf("scanPersistedStores() error = %v", err)
	}
	if finding != nil {
		t.Fatalf("scanPersistedStores() finding = %#v, want nil", finding)
	}
}

func TestScanPersistedStoresHonorsPerStoreIgnoreFile(t *testing.T) {
	storesRoot := t.TempDir()
	writeScanFixture(t, storesRoot, "demo", "overlay/.env", "AWS_ACCESS_KEY_ID="+syntheticAWSAccessKey+"\n")
	writeScanFixture(t, storesRoot, "demo", secretIgnoreFile, "aws-access-key:overlay/.env\n")

	finding, err := scanPersistedStores(storesRoot)
	if err != nil {
		t.Fatalf("scanPersistedStores() error = %v", err)
	}
	if finding != nil {
		t.Fatalf("scanPersistedStores() finding = %#v, want nil", finding)
	}
}

func writeScanFixture(t *testing.T, storesRoot, storeID, relativePath string, contents any) {
	t.Helper()
	path := filepath.Join(storesRoot, storeID, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}

	var data []byte
	switch value := contents.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		t.Fatalf("unsupported fixture content type %T", contents)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
