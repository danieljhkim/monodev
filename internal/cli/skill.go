package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/stores"
)

// skillName is the skill directory and frontmatter name. The Agent Skills
// specification requires the frontmatter name to match the directory.
const skillName = "monodev"

// skillTargets maps each --target value to the agent directory whose presence
// selects it by default and the SKILL.md path inside the workspace.
//
//   - claude: Claude Code loads project skills from .claude/skills/<name>/SKILL.md
//     (https://code.claude.com/docs/en/skills).
//   - agents: the open Agent Skills layout, which Codex scans as
//     .agents/skills/<name>/SKILL.md (https://agentskills.io/specification,
//     https://learn.chatgpt.com/docs/build-skills).
var skillTargets = []struct {
	name     string
	agentDir string
	path     string
}{
	{name: "claude", agentDir: ".claude", path: filepath.Join(".claude", "skills", skillName, "SKILL.md")},
	{name: "agents", agentDir: ".agents", path: filepath.Join(".agents", "skills", skillName, "SKILL.md")},
}

// skillCommandPaths lists the commands the skill names. Their usage and short
// help come from the command tree, so the skill tracks the real surface.
var skillCommandPaths = [][]string{
	{"context"},
	{"status"},
	{"checkout"},
	{"track"},
	{"save"},
	{"apply"},
	{"unapply"},
	{"push"},
	{"pull"},
}

var (
	skillTarget string
	skillForce  bool
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Install the monodev skill for coding agents",
	Long: `Install a SKILL.md that teaches coding agents the monodev loop.

The skill is generated from this binary, so it names the commands and flags
this version ships. Rewrite it with 'monodev skill init --force' after an
upgrade.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var skillInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write the monodev skill and track it in the active store",
	Long: `Write SKILL.md for coding agents and track it in the active store.

Targets:
  claude   .claude/skills/monodev/SKILL.md (Claude Code)
  agents   .agents/skills/monodev/SKILL.md (Agent Skills layout, read by Codex and others)

Without --target, writes each target whose agent directory (.claude/ or
.agents/) already exists in this workspace. The skill is tracked in the
active store, snapshotted, and hidden from git, so git status stays clean.
An existing SKILL.md is never overwritten without --force.`,
	Args: cobra.NoArgs,
	RunE: runSkillInit,
}

var skillShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the monodev skill to stdout",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprint(os.Stdout, renderSkill(rootCmd))
		return err
	},
}

func init() {
	skillInitCmd.Flags().StringVar(&skillTarget, "target", "", "Skill location: claude, agents, or all (default: targets whose agent directory exists)")
	skillInitCmd.Flags().BoolVarP(&skillForce, "force", "f", false, "Overwrite an existing SKILL.md")
	skillCmd.AddCommand(skillInitCmd)
	skillCmd.AddCommand(skillShowCmd)
}

// resolveSkillTargets returns the workspace-relative SKILL.md paths to write.
func resolveSkillTargets(workspaceRoot, target string) ([]string, error) {
	var paths []string
	switch target {
	case "":
		for _, t := range skillTargets {
			info, err := os.Stat(filepath.Join(workspaceRoot, t.agentDir))
			if err == nil && info.IsDir() {
				paths = append(paths, t.path)
			}
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("no agent directory (.claude/ or .agents/) in this workspace; pass --target claude, agents, or all")
		}
	case "all":
		for _, t := range skillTargets {
			paths = append(paths, t.path)
		}
	default:
		for _, t := range skillTargets {
			if t.name == target {
				paths = append(paths, t.path)
			}
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("invalid --target %q: want claude, agents, or all", target)
		}
	}
	return paths, nil
}

// coveringTrackedPath returns the tracked path that already contains relPath,
// or "" when none does. Committing that path snapshots the skill without
// tracking a second, nested entry for it.
func coveringTrackedPath(tracked []string, relPath string) string {
	for _, trackedPath := range tracked {
		clean := filepath.Clean(trackedPath)
		if clean == relPath || strings.HasPrefix(relPath, clean+string(filepath.Separator)) {
			return clean
		}
	}
	return ""
}

func runSkillInit(cmd *cobra.Command, args []string) error {
	eng, err := newEngine()
	if err != nil {
		return err
	}

	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	status, err := eng.Status(ctx, &engine.StatusRequest{CWD: cwd})
	if err != nil {
		return err
	}
	if status.ActiveStore == "" {
		return fmt.Errorf("%w: skill init tracks the skill in the active store; run 'monodev checkout -n <store>' first", engine.ErrNoActiveStore)
	}

	paths, err := resolveSkillTargets(cwd, skillTarget)
	if err != nil {
		return err
	}

	// Refuse before writing anything so a partial run never leaves one
	// target rewritten and another untouched.
	if !skillForce {
		for _, relPath := range paths {
			if _, err := os.Lstat(filepath.Join(cwd, relPath)); err == nil {
				return fmt.Errorf("%s already exists; rerun with --force to rewrite it", relPath)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to inspect %s: %w", relPath, err)
			}
		}
	}

	content := []byte(renderSkill(rootCmd))
	var toTrack, toCommit []string
	for _, relPath := range paths {
		absPath := filepath.Join(cwd, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", filepath.Dir(relPath), err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", relPath, err)
		}
		if covering := coveringTrackedPath(status.TrackedPaths, relPath); covering != "" {
			toCommit = append(toCommit, covering)
			continue
		}
		toTrack = append(toTrack, relPath)
		toCommit = append(toCommit, relPath)
	}

	if len(toTrack) > 0 {
		trackResult, err := eng.Track(ctx, &engine.TrackRequest{
			CWD:         cwd,
			Paths:       toTrack,
			Role:        stores.RoleDocs,
			Description: "monodev skill for coding agents",
			Origin:      stores.OriginUser,
		})
		if err != nil {
			return err
		}
		if len(trackResult.MissingPaths) > 0 {
			return fmt.Errorf("failed to track %s", strings.Join(trackResult.MissingPaths, ", "))
		}
	}

	commitResult, err := eng.Commit(ctx, &engine.CommitRequest{
		CWD:   cwd,
		Paths: uniqueStrings(toCommit),
		Adopt: true,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		return outputJSON(struct {
			Store     string   `json:"store"`
			Written   []string `json:"written"`
			Tracked   []string `json:"tracked"`
			Committed []string `json:"committed"`
			Version   string   `json:"version"`
			Warnings  []string `json:"warnings,omitempty"`
		}{
			Store:     status.ActiveStore,
			Written:   paths,
			Tracked:   nonNilStrings(toTrack),
			Committed: commitResult.Committed,
			Version:   rootCmd.Version,
			Warnings:  commitResult.Warnings,
		})
	}

	for _, warning := range commitResult.Warnings {
		PrintWarning(warning)
	}
	PrintSuccess(fmt.Sprintf("Wrote monodev skill (%s)", rootCmd.Version))
	PrintList(paths, 1)
	PrintLabelValue("Store", status.ActiveStore)
	PrintInfo("Tracked, snapshotted, and hidden from git. Share it with 'monodev push'.")
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// renderSkill builds SKILL.md from the command tree rooted at root, stamped
// with root's version.
func renderSkill(root *cobra.Command) string {
	version := root.Version
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + skillName + "\n")
	b.WriteString("description: Restore and save private agent notes, scripts and session handoffs with monodev, hidden from git. Use at session start in a repository that uses monodev, before ending a session, and before creating agent-only files.\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  monodev-version: %q\n", version)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "<!-- Generated by monodev %s. Regenerate after upgrading: monodev skill init --force -->\n\n", version)
	b.WriteString(`# monodev

monodev keeps files that do not belong in git in a store outside it, and
overlays them onto the checkout hidden from ` + "`git status`" + `. Notes and
scripts are plain files; there is no index or schema.

## What belongs here

- monodev: context that is private, unreviewed or experimental, such as your
  notes, scratch scripts and session handoffs.
- Not monodev: decisions that bind everyone (ADRs) and gotchas true for every
  contributor. Put those in reviewed, committed repository docs.

## Layout

- ` + "`.agents/notes/lessons.md`" + `: short durable lessons, edited in place.
- ` + "`.agents/notes/sessions/<date>-<agent>-<id>.md`" + `: one file per session.
  Never edit another session's file; parallel agents each write their own.
- ` + "`.agents/scripts/`" + `: helper scripts.

## Session start

1. Run ` + "`monodev context`" + ` (` + "`--json`" + ` for a stable shape) in the
   directory you work in. It reports the active store, or pulls and applies
   the stores an earlier agent pushed for this directory, then lists the
   notes, sessions and scripts on disk.
2. Read ` + "`lessons.md`" + ` and any session files. Check each note against the
   code before trusting it: notes can be stale.
3. If a file you expect is missing, restore it with ` + "`monodev apply`" + `.
   Do not recreate it.

## Session end

1. Write your session file under ` + "`.agents/notes/sessions/`" + `.
2. Fold anything durable into ` + "`lessons.md`" + `, then delete the session file.
3. Run ` + "`monodev save`" + `. It tracks new files under tracked directories and
   snapshots the store. The first time, track the directories you created,
   for example ` + "`monodev track .agents/notes .agents/scripts`" + `, then run
   ` + "`monodev apply`" + ` once so git stops listing them.
4. Sharing is a separate, deliberate step: ` + "`monodev push <store> --with-workspace`" + `
   publishes the store to the persistence branch, which anyone with repository
   access can read. It is not encrypted.

## Rules

- Never ` + "`git add`" + ` agent files or anything monodev manages. They are hidden
  from git on purpose.
- No active store: ` + "`monodev checkout -n <store>`" + `.
- ` + "`monodev unapply`" + ` deletes the store's files from the checkout.
- If ` + "`unapply`" + ` refuses because files drifted from the store, run
  ` + "`monodev save`" + ` first. Do not pass ` + "`--force`" + `.

## Commands
`)
	b.WriteString("\n")
	for _, path := range skillCommandPaths {
		cmd, _, err := root.Find(path)
		if err != nil || cmd == nil || cmd == root {
			continue
		}
		usage := cmd.CommandPath()
		if _, rest, ok := strings.Cut(cmd.Use, " "); ok {
			usage += " " + rest
		}
		fmt.Fprintf(&b, "- `%s`: %s\n", usage, cmd.Short)
	}
	return b.String()
}
