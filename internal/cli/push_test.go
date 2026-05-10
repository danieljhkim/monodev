package cli

import (
	"strings"
	"testing"

	monosync "github.com/danieljhkim/monodev/internal/sync"
)

func TestPrintPushResult_WorkspaceReferenceOutput(t *testing.T) {
	result := &monosync.PushResult{
		PushedWorkspace:  true,
		WorkspaceRefPath: ".monodev/persist/workspaces/workspace-123.json",
		CommitMessage:    "push: workspace",
		Remote:           "origin",
		Branch:           "monodev/persist",
	}

	output := captureStdout(t, func() {
		printPushResult(result, nil)
	})

	if !strings.Contains(output, "Pushed workspace reference: .monodev/persist/workspaces/workspace-123.json") {
		t.Fatalf("expected workspace artifact path in output, got:\n%s", output)
	}
}

func TestPrintPushResult_DoesNotClaimWorkspaceWithoutArtifact(t *testing.T) {
	result := &monosync.PushResult{
		PushedWorkspace: false,
		CommitMessage:   "push: store test-store",
		Remote:          "origin",
		Branch:          "monodev/persist",
	}

	output := captureStdout(t, func() {
		printPushResult(result, []string{"test-store"})
	})

	if strings.Contains(output, "Pushed workspace") {
		t.Fatalf("did not expect workspace success output, got:\n%s", output)
	}
}

func TestPrintPushResult_DryRunWorkspaceReferenceOutput(t *testing.T) {
	result := &monosync.PushResult{
		PushedWorkspace:  true,
		WorkspaceRefPath: ".monodev/persist/workspaces/workspace-123.json",
		DryRun:           true,
		CommitMessage:    "push: workspace",
		Remote:           "origin",
		Branch:           "monodev/persist",
	}

	output := captureStdout(t, func() {
		printPushResult(result, nil)
	})

	if !strings.Contains(output, "Would push workspace reference: .monodev/persist/workspaces/workspace-123.json") {
		t.Fatalf("expected dry-run workspace output, got:\n%s", output)
	}
	if strings.Contains(output, "Pushed workspace reference") {
		t.Fatalf("dry run should not claim the workspace reference was pushed, got:\n%s", output)
	}
}
