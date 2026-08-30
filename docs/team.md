# Team sharing through the persistence branch

Share stores across clones without putting overlay files on `main`. Persistence
uses a separate git orphan branch (`monodev/persist` by default) on a remote
you already have.

That branch is visible to anyone with repository access and is **not
encrypted**. Treat it as organizational isolation, not secrecy.

## One-time remote

On each clone:

```bash
monodev remote use origin
monodev remote show
```

`use` verifies the named remote exists in this repository. Optional custom
branch:

```bash
monodev remote set-branch monodev/custom
monodev remote set-branch monodev/persist
```

Config is repo-local at `.monodev/remote.json`.

## Author: capture and push

```bash
monodev checkout -n team-overlay --description "shared agent files"
monodev track --agents
monodev commit --all
monodev apply
git status
monodev push team-overlay
```

No store IDs pushes every local store:

```bash
monodev push
monodev push team-overlay --dry-run
```

Include the current workspace reference (active store and applied set):

```bash
monodev push team-overlay --with-workspace
```

`--force` overwrites the remote snapshot. `--allow-secrets` pushes after a
secret-scan finding; default is to refuse.

## Teammate: pull and apply

On another clone of the same remote:

```bash
monodev remote use origin
monodev pull team-overlay
monodev apply team-overlay
git status
```

The overlay files land in this clone and stay out of `git status`. Pull always
verifies checksums. If the local store already exists and differs, pull refuses
until you pass `--force`:

```bash
monodev pull team-overlay --force
```

Restore a pushed workspace reference and the stores it names:

```bash
monodev pull --workspace <remote-workspace-id> --with-stores
monodev apply
```

## Everyday sync

After a session, `sync` commits tracked paths, pushes, then pulls — that order,
so you do not have to remember it:

```bash
monodev sync
```

It fails fast if no remote is configured. `--allow-secrets` matches push.

```bash
monodev sync --allow-secrets
```

## What is shared

Push/pull move **store content** (tracked paths and overlay bytes). They do not
apply overlays on the other machine for you; `apply` is still local.

Workspace ledgers stay per checkout. Two clones can apply the same store and
unapply independently.

Default store location is `<repo>/.monodev/stores/` on each clone. Persistence
materializes a copy under `.monodev/persist/` for the orphan-branch commit;
that tree is gitignored with the rest of `.monodev`.

Command flags: [commands.md](commands.md). Solo loop: [solo.md](solo.md).
Parallel agents: [worktrees.md](worktrees.md).
