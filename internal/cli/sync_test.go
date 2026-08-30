package cli

import (
	"strings"
	"testing"
)

func TestSyncCommand_NoRemoteConfiguredFailsWithGuidance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")

	resetCommandFlags(rootCmd)
	rootCmd.SetArgs([]string{"sync"})
	var execErr error
	_ = captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})

	if execErr == nil {
		t.Fatal("expected sync to fail when no remote is configured")
	}
	if !strings.Contains(execErr.Error(), "monodev remote use") {
		t.Fatalf("error = %v, want it to name 'monodev remote use'", execErr)
	}
}

func TestSyncCommandRegistersAllowSecretsFlag(t *testing.T) {
	flag := syncCmd.Flags().Lookup("allow-secrets")
	if flag == nil {
		t.Fatal("sync command does not register --allow-secrets")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--allow-secrets default = %q, want false", flag.DefValue)
	}
}
