package remote

import "testing"

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
