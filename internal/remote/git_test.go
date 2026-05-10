package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateGitRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		refType string
		wantErr bool
	}{
		// Valid refs
		{name: "simple branch", ref: "main", refType: "branch", wantErr: false},
		{name: "branch with slash", ref: "feature/add-auth", refType: "branch", wantErr: false},
		{name: "branch with dots", ref: "release.1.0", refType: "branch", wantErr: false},
		{name: "branch with underscore", ref: "my_branch", refType: "branch", wantErr: false},
		{name: "branch with hyphen", ref: "my-branch", refType: "branch", wantErr: false},
		{name: "remote name", ref: "origin", refType: "remote", wantErr: false},
		{name: "monodev persist branch", ref: "monodev/persist", refType: "branch", wantErr: false},

		// Invalid refs
		{name: "empty", ref: "", refType: "branch", wantErr: true},
		{name: "starts with hyphen", ref: "-branch", refType: "branch", wantErr: true},
		{name: "contains space", ref: "my branch", refType: "branch", wantErr: true},
		{name: "contains semicolon", ref: "branch;rm -rf /", refType: "branch", wantErr: true},
		{name: "contains pipe", ref: "branch|cat /etc/passwd", refType: "branch", wantErr: true},
		{name: "contains backtick", ref: "branch`whoami`", refType: "branch", wantErr: true},
		{name: "contains dollar", ref: "branch$HOME", refType: "branch", wantErr: true},
		{name: "contains ampersand", ref: "branch&&echo", refType: "branch", wantErr: true},
		{name: "starts with dot", ref: ".hidden", refType: "branch", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitRef(tt.ref, tt.refType)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitRef(%q, %q) error = %v, wantErr %v", tt.ref, tt.refType, err, tt.wantErr)
			}
		})
	}
}

func TestRealGitPersistenceRunGitHonorsContextCancellation(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create fake git dir: %v", err)
	}

	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nsleep 5\n"), 0755); err != nil {
		t.Fatalf("failed to write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := NewRealGitPersistence().runGit(ctx, t.TempDir(), "status")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runGit error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runGit returned after %s, want prompt cancellation", elapsed)
	}
}
