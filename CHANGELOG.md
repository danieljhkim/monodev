

# Changelog

All notable changes to **monodev** will be documented in this file.

This project follows a pragmatic variant of [Keep a Changelog], but prioritizes
clarity over ceremony. Versions are pre-1.0 and may evolve rapidly.

---

## [Unreleased]

### Breaking Changes
- Removed `--owner`, `--task-id`, and `--scope` from `checkout -n`, `store update`, `store ls`, and `store rm`. Using a removed flag now errors with a message that names the flag as removed, rather than cobra's generic unknown-flag text.
- Store metadata no longer persists `owner`, `taskId`, or `scope`. Existing `meta.json` files that still contain those keys remain readable; the extra keys are ignored on load and omitted on the next write.
- `store describe` and `store ls` (including `--json`) no longer emit owner, taskId, or scope.
- Removed the `stack` command and all of its subcommands (`stack add`, `stack apply`, `stack unapply`, `stack ls`, `stack pop`, `stack clear`). Replacements: `monodev apply [store-id...]` (later stores win path conflicts), `monodev unapply [store-id...]`, and `monodev status` for the applied set. The hidden stubs error and name those replacements.
- Removed `monodev clear`. Replacement: `monodev workspace rm` with no argument deletes the current workspace.

### Added
- `monodev apply [store-id...]` and `monodev unapply [store-id...]` apply or remove several stores in one invocation. `unapply --all` removes every applied overlay in the workspace.
- `monodev save` tracks new files under already-tracked directories, then commits everything (`commit --all`). `--dry-run` previews both steps.
- `monodev sync` commits, pushes, then pulls the active store against the configured remote. Configure the remote first with `monodev remote use`.
- `monodev eject` detaches the current workspace. Default keeps overlay files on disk; `--remove-files` deletes them. Stores are retained in both modes (`store rm` remains the delete).
- `monodev doctor` reports drift and interrupted overlay transactions; `doctor --fix` applies the safe repairs.

### Changed
- New stores are created in the resolver default location: component (`<repo>/.monodev/stores/`) after `monodev init` or the first command that needs a state root, otherwise global (`~/.monodev/stores/`). There is no CLI flag to override that choice; set `MONODEV_ROOT=$HOME/.monodev` to keep using the home-directory root.
- README leads with the agent-context problem and a before/after `git status`. The exhaustive command list moved to `docs/commands.md`. Workflow pages: `docs/solo.md`, `docs/team.md`, `docs/worktrees.md`.

---

## [0.2.8] — 2026-08-22

### Security
- Overlay apply can no longer write into `.git/`, which previously allowed RCE via git hooks.
- Store directories that hold dev-only artifacts are no longer created world-readable.

### Fixed
- `monodev pull` applies store content from the shared remote instead of leaving the workspace unchanged.
- Duplicate scoped stores no longer appear in `store ls`.
- Apply prefers the explicit store ID when resolving which overlay to write.

### Changed
- Module and release builds use Go 1.27; CI runs `govulncheck`.
- CI pins `golangci-lint` v2.13.1 (Go 1.27 support). Code scanning uses an advanced CodeQL workflow that installs Go from `go.mod` before analysis.

---

## [0.2.7] — 2026-02-28

### Changed
- Removed deprecated `StoreMeta` fields: `type`, `status`, `priority`, `source`, and `parentTaskId`. Existing `meta.json` files with these fields are silently ignored on load.
- `monodev store ls` now displays: Name, Scope, Owner, Description. Removed verbose mode (`-v`) and filters for removed fields.
- `monodev store describe` no longer prints Source, Type, Priority, Status, or Parent Task ID.
- `monodev store update` now accepts only `--description`, `--owner`, and `--task-id` flags.
- `monodev checkout -n` now accepts only `--description`, `--owner`, and `--task-id` flags.

### Fixed
- Commands that reuse the active store, including `monodev track`, now fall back to global scope when workspace state still records `component` but the current repository has no `.monodev` directory.

---

## [0.2.6] — 2026-02-28

### Fixed
- Overlay paths are now stored relative to the workspace directory (CWD at track time) instead of the repository root. This makes stores portable across directories: applying a store from `packages/api/` correctly places files in `packages/api/`, even if the store was originally tracked from `packages/web/`.
- `monodev unapply` now removes files from the correct workspace subdirectory.
- `monodev apply <store-id>` no longer requires a prior `monodev use <store-id>` checkout. The store is resolved directly by ID.

## [0.2.5] — 2026-02-08

### Changed
- Added more fields to meta.json and track.json: source, type, owner, taskId, parentTaskId, priority, status, etc.
- Updated `monodev store describe` & `monodev store ls` to show more fields.

## Added
- New `monodev store update` command for updating store metadata.

## [0.2.4] — 2026-02-06

### Added
- New `monodev clear` command for deleting workspace state files with `--force` and `--dry-run` flags.
- Custom help function with colored output for improved CLI readability, including grouped command listings and flags usage.
- Command groups for better organization: workspace lifecycle, store operations, stack management, workspace management, remote persistence, and CLI tooling.

### Changed
- Enhanced CLI help output with colored group titles and section headers for better readability.
- Improved command organization with grouped and ungrouped sections in help output.

## [0.2.2] — 2026-01-31

### Added
- New `monodev diff` command for comparing workspace and store overlays.

## [0.2.1] — 2026-01-31

### Breaking Changes
- Reorganized CLI commands into parent commands:
  - `monodev list` → `monodev store ls`
  - `monodev delete <store-id>` → `monodev store rm <store-id>`
  - `monodev describe <store-id>` → `monodev store describe <store-id>`
  - Removed `monodev prune` command (not registered, functionality removed)
- Added new `workspace` parent command for managing workspace state:
  - `monodev workspace ls` - List all workspaces
  - `monodev workspace describe <workspace-id>` - Show workspace details
  - `monodev workspace rm <workspace-id>` - Delete workspace state file
- Removed symlink support for now.

### Added
- New engine methods for workspace management:
  - `ListWorkspaces()` - Enumerate all workspace state files
  - `DescribeWorkspace()` - Get detailed workspace information
  - `DeleteWorkspace()` - Delete workspace state file with safety checks
- Support for non-git repositories.

## [0.2.0] — 2026-01-25

### Added
- Stack commands (stack apply/unapply) for managing multiple stores in one go.

### Changed
- Renamed `use` to `checkout` for clarity.
- Renamed `save` to `commit` for clarity.
- Removed 'copy' mode for now.
- Better error handling and output formatting.
- `monodev apply/unapply` only works on the "active store" now.

## [0.1.0] — Initial release

### Added
- Core CLI scaffolding (`monodev`) with explicit command surface.
- Store model for reusable, local-only development overlays.
- Store activation via `monodev use` and `monodev use -n`.
- Stack-based composition of stores with deterministic precedence.
- Tracking of workspace-relative paths via `track.json`.
- Safe persistence of dev artifacts into stores via `monodev save`.
- Explicit workspace mutation boundaries:
  - `monodev apply` to materialize overlays
  - `monodev unapply` to remove applied overlays
- Support for `symlink` (default) and `copy` overlay modes.
- Workspace state ledger to track ownership and enable safe unapply.
- Conflict detection with explicit `--force` escape hatch.
- Status and inspection commands (`status`, `list`, `describe`).

### Design principles
- Local-only by default; no network access.
- Clear separation between intent (`track`, `save`) and mutation (`apply`, `unapply`).

### Notes
- This is an early release intended for users working in large monorepos.
- Backward compatibility is not guaranteed prior to 1.0.
- Feedback on ergonomics, edge cases, and failure modes is welcome.

[Keep a Changelog]: https://keepachangelog.com/en/1.0.0/
