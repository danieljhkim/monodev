package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

// agentContextPresetPaths is the single source of truth for paths commonly
// used to configure coding agents. A trailing slash is kept for directory
// entries so skipped paths are reported in the same form users recognize.
var agentContextPresetPaths = []string{
	".claude/",
	"CLAUDE.md",
	".cursor/",
	".cursorrules",
	"AGENTS.md",
	".github/copilot-instructions.md",
	".aider*",
	".codex/",
	".gemini/",
}

var trackAgents bool

// resolveAgentPresetPaths resolves the agent preset against the supplied
// filesystem. It deliberately accepts fs.FS so tests can use an in-memory
// fixture and the CLI can scope resolution to the current workspace.
func resolveAgentPresetPaths(workspace fs.FS) (found, missing []string, err error) {
	seen := make(map[string]bool)
	for _, presetPath := range agentContextPresetPaths {
		pattern := strings.TrimSuffix(presetPath, "/")
		matches, globErr := fs.Glob(workspace, pattern)
		if globErr != nil {
			return nil, nil, fmt.Errorf("failed to resolve agent context path %q: %w", presetPath, globErr)
		}
		if len(matches) == 0 {
			missing = append(missing, presetPath)
			continue
		}
		for _, match := range matches {
			if !seen[match] {
				found = append(found, match)
				seen[match] = true
			}
		}
	}
	return found, missing, nil
}

var trackCmd = &cobra.Command{
	Use:   "track [path]...",
	Short: "Track paths in the active store",
	Long:  `Add paths to the active store's track file. Paths are resolved relative to the repository root. Use --agents to track existing agent context paths.`,
	Args: func(cmd *cobra.Command, args []string) error {
		agents, err := cmd.Flags().GetBool("agents")
		if err != nil {
			return err
		}
		if len(args) == 0 && !agents {
			return fmt.Errorf("must specify paths to track or use --agents flag")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		paths := append([]string(nil), args...)
		var agentFound, agentMissing []string
		agents, err := cmd.Flags().GetBool("agents")
		if err != nil {
			return err
		}
		if agents {
			agentFound, agentMissing, err = resolveAgentPresetPaths(os.DirFS(cwd))
			if err != nil {
				return err
			}
			paths = append(agentFound, paths...)
		}

		role, _ := cmd.Flags().GetString("role")
		description, _ := cmd.Flags().GetString("description")
		origin, _ := cmd.Flags().GetString("origin")

		req := &engine.TrackRequest{
			CWD:         cwd,
			Paths:       paths,
			Role:        role,
			Description: description,
			Origin:      origin,
		}

		result, err := eng.Track(ctx, req)
		if err != nil {
			return err
		}

		allMissing := append([]string(nil), result.MissingPaths...)
		allMissing = append(allMissing, agentMissing...)

		if jsonOutput {
			resolvedPaths := make([]string, 0, len(result.ResolvedPaths))
			for _, resolved := range result.ResolvedPaths {
				resolvedPaths = append(resolvedPaths, resolved)
			}
			jsonResult := struct {
				TrackedPaths      []string `json:"trackedPaths"`
				MissingPaths      []string `json:"missingPaths,omitempty"`
				AgentFoundPaths   []string `json:"agentFoundPaths,omitempty"`
				AgentSkippedPaths []string `json:"agentSkippedPaths,omitempty"`
				Count             int      `json:"count"`
			}{
				TrackedPaths:      resolvedPaths,
				MissingPaths:      allMissing,
				AgentFoundPaths:   agentFound,
				AgentSkippedPaths: agentMissing,
				Count:             len(resolvedPaths),
			}
			return outputJSON(jsonResult)
		}

		if agents {
			if len(agentFound) > 0 {
				PrintInfo(fmt.Sprintf("Agent paths found: %s", strings.Join(agentFound, ", ")))
			}
			for _, missing := range agentMissing {
				PrintWarning(fmt.Sprintf("Agent path skipped-absent: %s", missing))
			}
			if len(agentFound) == 0 {
				PrintInfo("No agent context paths found")
			}
		}

		// Warn about missing paths
		for _, missing := range result.MissingPaths {
			PrintWarning(fmt.Sprintf("Path not found in workspace: %s", missing))
		}

		trackedCount := len(result.ResolvedPaths)
		if trackedCount > 0 {
			// Show resolved paths when they differ from input
			for _, arg := range args {
				resolved := result.ResolvedPaths[arg]
				if resolved != "" && resolved != arg {
					PrintInfo(fmt.Sprintf("  %s → %s", arg, resolved))
				}
			}
			PrintSuccess(fmt.Sprintf("Tracked %s", PrintCount(trackedCount, "path", "paths")))
		} else {
			PrintWarning("No paths tracked")
		}
		return nil
	},
}

func init() {
	trackCmd.Flags().BoolVar(&trackAgents, "agents", false, "Track existing AI agent context paths")
	trackCmd.Flags().String("role", "", "Path role (script, docs, style, config, other)")
	trackCmd.Flags().String("description", "", "Description of the tracked path")
	trackCmd.Flags().String("origin", "", "Origin of the tracked path (user, agent, other)")
}
