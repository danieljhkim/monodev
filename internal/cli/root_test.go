package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpCommand_CommandPaths(t *testing.T) {
	originalOut, originalErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	t.Cleanup(func() {
		resetCommandFlags(rootCmd)
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(originalOut)
		rootCmd.SetErr(originalErr)
	})
	execute := func(args []string) (string, error) {
		resetCommandFlags(rootCmd)
		rootCmd.SetArgs(args)
		var output bytes.Buffer
		rootCmd.SetOut(&output)
		rootCmd.SetErr(&output)
		err := rootCmd.Execute()
		return output.String(), err
	}

	for _, tt := range []struct {
		name string
		path []string
		want []string
	}{
		{
			name: "nested deletion command",
			path: []string{"store", "rm"},
			want: []string{"monodev store rm <store-id>", "Delete a store permanently", "--force", "--dry-run", "--help"},
		},
		{
			name: "command group",
			path: []string{"store"},
			want: []string{"monodev store", "rm", "describe"},
		},
		{
			name: "root",
			want: []string{"monodev", "Workspace Lifecycle:", "CLI & Tooling:", "help"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := execute(append([]string{"help"}, tt.path...))
			if err != nil {
				t.Fatalf("help %q error = %v", tt.path, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("help %q output = %q, want %q", tt.path, output, want)
				}
			}
			direct, err := execute(append(append([]string{}, tt.path...), "--help"))
			if err != nil {
				t.Fatalf("%q --help error = %v", tt.path, err)
			}
			if output != direct {
				t.Errorf("help %q differs from --help:\nhelp: %s\n--help: %s", tt.path, output, direct)
			}
		})
	}

	for _, tt := range []struct {
		name string
		path []string
		want string
	}{
		{name: "unknown root command", path: []string{"does-not-exist"}, want: "monodev"},
		{name: "unknown nested command", path: []string{"store", "does-not-exist"}, want: "monodev store"},
		{name: "unknown trailing segment", path: []string{"store", "rm", "does-not-exist"}, want: "monodev store rm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := execute(append([]string{"help"}, tt.path...))
			if err == nil {
				t.Fatalf("help %q returned nil; output = %q", tt.path, output)
			}
			if !strings.Contains(err.Error(), "unknown command") ||
				!strings.Contains(err.Error(), "does-not-exist") ||
				!strings.Contains(err.Error(), tt.want) {
				t.Errorf("help %q error = %q, want unknown command and parent %q", tt.path, err, tt.want)
			}
			if output != "" {
				t.Errorf("help %q printed help for an unresolved path: %q", tt.path, output)
			}
		})
	}
}

func TestRootCommand_Help(t *testing.T) {
	rootCmd.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("expected help output, got empty string")
	}
	if !contains(output, "monodev") {
		t.Error("expected help to contain 'monodev'")
	}
}

func TestRootCommand_Version(t *testing.T) {
	SetVersion("1.2.3")
	// Cobra uses --version flag, not a version subcommand
	rootCmd.SetArgs([]string{"--version"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Version output should contain the version number
	if !contains(output, "1.2.3") && !contains(output, "dev") {
		t.Errorf("expected version output to contain version, got %q", output)
	}
}

func TestRootCommand_InvalidCommand(t *testing.T) {
	rootCmd.SetArgs([]string{"invalid-command"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid command")
	}
}

func TestSetVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"normal version", "1.2.3", "1.2.3"},
		{"empty version", "", "dev"}, // Should not change if empty
		{"dev version", "dev", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetVersion(tt.version)
			if tt.version != "" && rootCmd.Version != tt.version {
				t.Errorf("SetVersion(%q) = %q, want %q", tt.version, rootCmd.Version, tt.version)
			}
		})
	}
}

func TestRootCommand_Subcommands(t *testing.T) {
	subcommands := []string{
		"apply", "unapply", "status", "checkout", "track", "untrack",
		"commit", "store", "workspace",
	}

	for _, cmd := range subcommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{cmd})
			if err != nil {
				t.Errorf("Find(%q) error = %v", cmd, err)
			}
			if subCmd == nil {
				t.Errorf("Find(%q) returned nil command", cmd)
			}
		})
	}
}

func TestStoreCommand_Subcommands(t *testing.T) {
	storeSubcommands := []string{"ls", "rm", "describe"}

	for _, cmd := range storeSubcommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{"store", cmd})
			if err != nil {
				t.Errorf("Find(%q) error = %v", []string{"store", cmd}, err)
			}
			if subCmd == nil {
				t.Errorf("Find(%q) returned nil command", []string{"store", cmd})
			}
		})
	}
}

func TestWorkspaceCommand_Subcommands(t *testing.T) {
	workspaceSubcommands := []string{"ls", "rm", "describe", "repair"}

	for _, cmd := range workspaceSubcommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{"workspace", cmd})
			if err != nil {
				t.Errorf("Find(%q) error = %v", []string{"workspace", cmd}, err)
			}
			if subCmd == nil {
				t.Errorf("Find(%q) returned nil command", []string{"workspace", cmd})
			}
		})
	}
}

func TestOldCommands_NotRegistered(t *testing.T) {
	oldCommands := []string{"list", "delete", "describe", "prune"}

	for _, cmd := range oldCommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{cmd})
			// These commands should not be found at the root level
			if err == nil && subCmd != nil && subCmd.Name() == cmd {
				t.Errorf("Old command %q should not be registered at root level", cmd)
			}
		})
	}
}

func TestCommandGroups_RejectUnexpectedArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "store retired command", args: []string{"store", "prune"}},
		{name: "store unknown command", args: []string{"store", "bogus"}},
		{name: "workspace unknown command", args: []string{"workspace", "bogus"}},
		{name: "remote unknown command", args: []string{"remote", "bogus"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCommandFlags(rootCmd)
			rootCmd.SetArgs(tt.args)
			var output bytes.Buffer
			rootCmd.SetOut(&output)
			rootCmd.SetErr(&output)

			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("Execute(%q) returned nil; output = %q", tt.args, output.String())
			}
			if !strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "unexpected argument") {
				t.Fatalf("Execute(%q) error = %q, want an unknown-command or unexpected-argument error", tt.args, err)
			}
		})
	}
}

func TestCommandGroups_Help(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "store", args: []string{"store"}},
		{name: "workspace", args: []string{"workspace"}},
		{name: "remote", args: []string{"remote"}},
		{name: "store help", args: []string{"store", "--help"}},
		{name: "workspace help", args: []string{"workspace", "--help"}},
		{name: "remote help", args: []string{"remote", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCommandFlags(rootCmd)
			rootCmd.SetArgs(tt.args)
			var output bytes.Buffer
			rootCmd.SetOut(&output)

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("Execute(%q) error = %v", tt.args, err)
			}
			if !strings.Contains(output.String(), "Usage:") {
				t.Fatalf("Execute(%q) output = %q, want help output", tt.args, output.String())
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr))))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
