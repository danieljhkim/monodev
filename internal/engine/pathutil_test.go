package engine

import (
	"testing"
)

func TestResolveToWorkspaceRelative(t *testing.T) {
	tests := []struct {
		name     string
		userPath string
		cwd      string
		repoRoot string
		want     string
		wantErr  bool
	}{
		{
			name:     "simple file at workspace root",
			userPath: "file.txt",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "file.txt",
		},
		{
			name:     "nested path under workspace",
			userPath: "src/index.ts",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "src/index.ts",
		},
		{
			name:     "repo root workspace, simple file",
			userPath: "docs/readme.md",
			cwd:      "/repo",
			repoRoot: "/repo",
			want:     "docs/readme.md",
		},
		{
			name:     "repo root workspace, simple file at root",
			userPath: "Makefile",
			cwd:      "/repo",
			repoRoot: "/repo",
			want:     "Makefile",
		},
		{
			name:     "absolute path inside workspace",
			userPath: "/repo/packages/web/config.yaml",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "config.yaml",
		},
		{
			name:     "path with redundant components within workspace",
			userPath: "./foo/../bar/baz.txt",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "bar/baz.txt",
		},
		{
			name:     "path escaping workspace via dotdot - rejected",
			userPath: "../api/config.yaml",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "path escaping repo entirely - rejected",
			userPath: "../../../outside.txt",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "absolute path outside repo - rejected",
			userPath: "/other/repo/file.txt",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "workspace root itself - rejected",
			userPath: ".",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveToWorkspaceRelative(tt.userPath, tt.cwd, tt.repoRoot)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (result: %q)", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveToRepoRelative(t *testing.T) {
	tests := []struct {
		name     string
		userPath string
		cwd      string
		repoRoot string
		want     string
		wantErr  bool
	}{
		{
			name:     "simple relative path in repo root",
			userPath: "file.txt",
			cwd:      "/repo",
			repoRoot: "/repo",
			want:     "file.txt",
		},
		{
			name:     "relative path in subdirectory",
			userPath: "config.yaml",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "packages/web/config.yaml",
		},
		{
			name:     "dotdot path to parent",
			userPath: "../../Makefile",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "Makefile",
		},
		{
			name:     "dotdot path to sibling directory",
			userPath: "../api/config.yaml",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "packages/api/config.yaml",
		},
		{
			name:     "absolute path inside repo",
			userPath: "/repo/scripts/build.sh",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "scripts/build.sh",
		},
		{
			name:     "path with redundant slashes",
			userPath: "./foo/../bar/baz.txt",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			want:     "packages/web/bar/baz.txt",
		},
		{
			name:     "outside repo - rejected",
			userPath: "../../../outside.txt",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "absolute path outside repo - rejected",
			userPath: "/other/repo/file.txt",
			cwd:      "/repo",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "repo root itself - rejected",
			userPath: ".",
			cwd:      "/repo",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "dotdot to repo root - rejected",
			userPath: "../..",
			cwd:      "/repo/packages/web",
			repoRoot: "/repo",
			wantErr:  true,
		},
		{
			name:     "nested path from repo root",
			userPath: "packages/web/src/index.ts",
			cwd:      "/repo",
			repoRoot: "/repo",
			want:     "packages/web/src/index.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveToRepoRelative(tt.userPath, tt.cwd, tt.repoRoot)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (result: %q)", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
