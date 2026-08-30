# Workspace identity

A workspace ID is `SHA-256(repoFingerprint + "|" + relativePath)`. The
relative path is from the repository root to the directory where a command
runs. Changing directory inside the repo produces a different workspace; the
clone's location on disk does not.

## Repository fingerprint

The fingerprint is chosen in this order:

1. **Durable repo ID**, when present. `monodev init` writes
   `.monodev/repo-id`. Remote-less repos also persist an ID at
   `<git-common-dir>/monodev/repo-id` on first use. A durable ID wins over
   remotes, so later remote edits do not change identity.
2. **Normalized remote URL**, when no durable ID exists.
   `origin` is used when it is configured. Otherwise the first name reported
   by `git remote` is used.
3. **Absolute path**, only when the directory is not a git repository.

The clone's absolute path is not hashed in cases 1–2. Moving a clone keeps the
fingerprint. See [docs/worktrees.md](worktrees.md) for what a linked git
worktree gets instead — it shares the repo-identity material above (the
durable ID or remote URL are read from the common git dir, so they are
identical across worktrees) but a worktree-specific suffix is still mixed in
before hashing, so each worktree's fingerprint — and therefore its
applied-overlay ledger — is distinct from the main checkout's and from every
other worktree's.

## Remote URL normalization

Equivalent remotes hash to the same value. Normalization:

- strips scheme, credentials, query, and fragment
- lowercases the host
- drops default ports (`22`, `80`, `443`)
- strips a trailing `.git` suffix and trailing slashes
- treats `git@host:org/repo` and `https://host/org/repo.git` as identical

Local paths and `file://` URLs are cleaned and have `.git` / trailing slashes
removed. Path case is preserved.

Examples that share a fingerprint:

- `git@github.com:org/repo.git`
- `ssh://git@github.com/org/repo.git`
- `https://github.com/org/repo.git/`
- `https://user:token@GitHub.com/org/repo.git`

## Multiple remotes

`origin` is preferred. If `origin` is absent, the first name from `git remote`
is used. Additional remotes (for example `upstream`) do not participate.

An org rename or a move to an unrelated URL still changes the remote-based
fingerprint. After `monodev init` (or any durable repo ID), remote churn does
not change identity.

## Moved clones

Because the absolute path is not part of a git repo's fingerprint, copying or
moving a clone keeps the same workspace IDs. Workspace JSON still records
`absolutePath` for display; that field is updated on the next load.

Two independent clones of the same remote that have not been initialized share
workspace files under `~/.monodev/workspaces/` when they use the same relative
path. `monodev init` gives each clone its own durable ID.

## Legacy files

Before this rule, fingerprints were `SHA-256(absoluteRoot + "|" + rawOriginURL)`
with the literal `unknown` when origin was missing. Loading a workspace looks
up that legacy ID and rewrites the file under the current ID, preserving the
active store and applied-overlay ledger.

## Repair

When automatic migration cannot find the old file (for example the remote was
rewritten before upgrading), list and rebind orphans:

```bash
monodev workspace repair
monodev workspace repair --rebind <workspace-id>
```

Repair selects workspace files whose `absolutePath` or `workspacePath` belongs
to the current repository and whose stored identity no longer matches. Rebind
writes the record under the current fingerprint and keeps `activeStore`,
`appliedStores`, and `paths`.
