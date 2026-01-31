

# Changelog

All notable changes to **monodev** will be documented in this file.

This project follows a pragmatic variant of [Keep a Changelog], but prioritizes
clarity over ceremony. Versions are pre-1.0 and may evolve rapidly.

---

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