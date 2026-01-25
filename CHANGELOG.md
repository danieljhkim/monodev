

# Changelog

All notable changes to **monodev** will be documented in this file.

This project follows a pragmatic variant of [Keep a Changelog], but prioritizes
clarity over ceremony. Versions are pre-1.0 and may evolve rapidly.

---

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