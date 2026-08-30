# Parallel agents with git worktrees

Running several coding agents at once, each in its own `git worktree`, is a
common way to parallelize work on one repository. This page covers what
monodev shares across those worktrees automatically, what it does not, and
the command sequence to get a new worktree's dev-only files in place.

## What's shared, what isn't

A [workspace ID](workspace-identity.md) is derived from the repository
fingerprint and the relative path from the repo root to the current
directory. Two linked worktrees of the same repository, at the same relative
path (typically the repo root), share:

- **The repo-identity material** — the durable repo ID (or remote URL) is
  read from the shared git-common-dir, so it is identical across every
  worktree and the main checkout.
- **Store content, for global-scope stores** — a global store's tracked
  files live once, addressed by store ID, under `~/.monodev/stores/<id>/`,
  independent of any workspace. Track and commit a dev-only file from the
  main checkout once; every worktree can apply it by name. Component-scoped
  stores are the exception: they live under `<repo-root>/.monodev/stores/`,
  which is part of the working tree itself, so each worktree gets its own
  (empty, until you commit into it) copy. Use a global-scope store (the
  default when the repo has no `.monodev/` directory) for anything you want
  a new worktree to pick up for free. After `monodev init`, new stores are
  created in the component location instead.

They do **not** share:

- **The applied-overlay ledger** — which store is checked out as active, and
  which paths are currently applied to a given working tree. Each linked
  worktree gets its own workspace fingerprint (a worktree-specific suffix is
  mixed in — see [workspace-identity.md](workspace-identity.md)) and
  therefore its own ledger file under `~/.monodev/workspaces/`. This is
  intentional: each worktree is a separate working tree on disk, and
  `unapply` in one must never delete files that only exist in another.

## Workflow

Track and commit a dev-only file once, from wherever you normally work:

```bash
cd ~/src/myrepo
monodev checkout -n dev-overlay
monodev track .env.local
monodev commit
monodev apply
```

Create the store *before* `monodev init` if you want it in the global
location (`~/.monodev/stores/`), which is shared across worktrees. After
`monodev init`, new stores default to component scope under
`<repo>/.monodev/stores/` and are not shared — see below.

Spin up a worktree for a parallel agent run — inside the repo, or anywhere
else on disk:

```bash
git worktree add ~/src/myrepo-agent-2 -b agent-2-work
cd ~/src/myrepo-agent-2
```

The new worktree has never applied anything, so it has no active store of its
own yet. Point it at the same store by ID and apply — the store's content is
already there, so this is just a checkout and copy, not a re-track/re-commit:

```bash
monodev apply dev-overlay
```

`.env.local` now exists in the new worktree. From here on, `monodev apply`
with no arguments in this worktree reuses `dev-overlay` as its active store,
exactly as it would in the main checkout — the two are independent, not
linked.

## Unapplying is per-worktree

Because the ledger is per-worktree, cleaning up one agent's worktree does not
touch another:

```bash
cd ~/src/myrepo-agent-2
monodev unapply
```

This removes `.env.local` from `myrepo-agent-2` only. The main checkout (and
any other worktree) keeps its own applied files untouched.

## Concurrency

Two worktrees applying the same store at the same time do not contend: each
Apply takes an exclusive lock on its own (worktree-specific) workspace ledger
and only a shared (read) lock on the store, so parallel agents pulling in the
same overlay never see a lock-contention error from each other.

## Removing a worktree

`git worktree remove` deletes the worktree's files but not its monodev
ledger. If you are done with a worktree for good, unapply first, or clean up
the orphaned ledger later with `monodev workspace ls` / `monodev workspace
rm <workspace-id>`.
