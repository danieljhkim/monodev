// Package planner handles the planning phase of overlay operations.
//
// The planner generates deterministic execution plans for applying and unapplying
// store overlays. It detects conflicts, validates paths, and determines the order
// of operations needed to safely apply or remove overlays.
//
// Key responsibilities:
//   - Generate ApplyPlan with ordered operations
//   - Detect conflicts (unmanaged files, type mismatches, mode mismatches)
//   - Handle store-to-store precedence and overrides
//   - Validate path safety before operations
package planner
