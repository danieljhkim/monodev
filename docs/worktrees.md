# Parallel agents with git worktrees

Running several coding agents at once, each in its own `git worktree`, is a
common way to parallelize work on one repository. This page covers what is
shared across those worktrees, what is not, and a command sequence that
actually applies the same overlay in a new worktree.

## What's shared, what isn't

A [workspace ID](workspace-identity.md) is derived from the repository
fingerprint and the relative path from the repo root to the current directory.
Two linked worktrees of the same repository, at the same relative path
(typically the repo root), share:

- **The repo-identity material** — the durable repo ID (or remote URL) is
  read from the shared git-common-dir, so it is identical across every
  worktree and the main checkout.
- **Store content, when the store lives outside the worktree** — a store
  addressed by ID under `MONODEV_ROOT` (commonly `$HOME/.monodev`) is visible
  from every worktree. Track and commit once; each worktree applies by name.

They do **not** share:

- **The applied-overlay ledger** — which store is active, and which paths are
  currently applied. Each linked worktree gets its own workspace fingerprint
  (a worktree-specific suffix is mixed in) and therefore its own ledger file.
  `unapply` in one must never delete files that only exist in another.
- **Component-scoped stores** — the default after the first command, which
  auto-creates `<worktree>/.monodev/stores/`. That directory is part of the
  working tree, so a new worktree starts with an empty copy. There is no
  `--scope` flag; `--scope` has been removed.

To share a store across worktrees on one machine, set `MONODEV_ROOT` **before**
creating it (so repo-local `.monodev` is not auto-created):

```bash
export MONODEV_ROOT="$HOME/.monodev"
```

Existing home-directory stores stay visible even when a clone later grows a
repo-local `.monodev`. The other way to share is the persistence branch:
[team.md](team.md).

## Workflow

Create a store that every worktree can see:

```bash
export MONODEV_ROOT="$HOME/.monodev"
cd ~/src/myrepo
monodev checkout -n dev-overlay
monodev track .env.local
monodev commit --all
monodev apply
git status
```

Spin up a worktree for a parallel agent — inside the repo, or anywhere else
on disk:

```bash
git worktree add ~/src/myrepo-agent-2 -b agent-2-work
cd ~/src/myrepo-agent-2
export MONODEV_ROOT="$HOME/.monodev"
```

The new worktree has never applied anything. Point it at the same store by ID
and apply — this is a checkout of already-committed overlay bytes, not a
re-track:

```bash
monodev apply dev-overlay
git status
```

`.env.local` now exists in the new worktree and `git status` is clean there.
The two workspaces are independent.

Without `MONODEV_ROOT`, push from the main checkout and pull in the worktree
instead:

```bash
monodev remote use origin
monodev push dev-overlay
cd ~/src/myrepo-agent-2
monodev remote use origin
monodev pull dev-overlay
monodev apply dev-overlay
```

## Unapplying is per-worktree

```bash
cd ~/src/myrepo-agent-2
monodev unapply
```

This removes `.env.local` from `myrepo-agent-2` only. The main checkout keeps
its own copy of the file.

Git worktrees share `.git/info/exclude` (it lives in the common git directory).
Unapply in one worktree updates that shared exclude list, so `git status` in
another worktree may show the overlaid files as untracked even though those
files are still on disk. Re-apply there, or run `monodev doctor --fix`, to
restore the exclude block.

Re-apply by id after unapply:

```bash
monodev apply dev-overlay
```

## Concurrency

Two worktrees applying the same store at the same time do not contend: each
apply takes an exclusive lock on its own (worktree-specific) workspace ledger
and only a shared lock on the store.

## Removing a worktree

`git worktree remove` deletes the worktree's files but not its monodev
ledger. Unapply first, or clean up later:

```bash
monodev workspace ls
monodev workspace rm <workspace-id>
```

Command flags: [commands.md](commands.md). Identity details:
[workspace-identity.md](workspace-identity.md).
