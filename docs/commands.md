# Command reference

Every command and flag below is present in `monodev help` / `<command> --help`
from the built binary. Hidden retired commands (`stack`, `clear`) are omitted
here; they error and name their replacements. Historical names `use` and `save`
from 0.1.0 are in the [CHANGELOG](../CHANGELOG.md); the shipped verbs are
`checkout`, `commit`, and the new `save` (track-new-then-commit).

Global flags, inherited by every command: `--json` (JSON output), `-h` /
`--help`. Root also has `-v` / `--version`.

```bash
monodev version
```

`monodev init` is optional. The first command that needs a state root creates
`<repo>/.monodev/{stores,workspaces}` (mode 0700) and a `*` gitignore.

---

## Workspace lifecycle

### apply

```bash
monodev apply
monodev apply store-a store-b
monodev apply --force
monodev apply --dry-run
```

No arguments: apply the active store. Store IDs: apply in argument order; later
stores win path conflicts. `--force` (`-f`) overrides conflicts. `--dry-run`
prints the plan and writes nothing.

### unapply

```bash
monodev unapply
monodev unapply store-a
monodev unapply --all
monodev unapply --force
monodev unapply --dry-run
```

No arguments: remove paths owned by the active store. Store IDs: remove only
those owners. `--all` removes every applied overlay in this workspace and cannot
be combined with store IDs. `--force` (`-f`), `--dry-run`.

### status

```bash
monodev status
```

Active workspace, applied stores in ledger order, path ownership, and tracked
path flags (applied / committed / modified).

### diff

```bash
monodev diff
monodev diff --patch
monodev diff --name-only
monodev diff --name-status
monodev diff --store-id other-store
```

`--patch` (`-p`) unified diff. `--name-only` / `--name-status` names only.
`--store-id` (`-s`) selects a store; default is the active store.

### doctor

```bash
monodev doctor
monodev doctor --fix
```

Read-only drift and interrupted-transaction report. `--fix` applies safe
repairs (complete or roll back a pending overlay journal, prune ledger entries
for deleted stores, remove orphaned backups, reconcile `.git/info/exclude`).
Exits non-zero when problems remain. Journal details:
[overlay-recovery.md](overlay-recovery.md).

### eject

```bash
monodev eject --dry-run
monodev eject
monodev eject --yes
monodev eject --keep-files
monodev eject --remove-files
monodev eject --remove-files --dry-run
```

Detach this workspace. Stores are never deleted (`store rm` remains explicit).
Default is keep-files: leave current overlay bytes on disk, drop the ownership
ledger, remove the managed exclude block. `--keep-files` is that default.
`--remove-files` deletes every overlaid path. Confirmation is required unless
`--yes` or `--dry-run`. `--json` requires `--yes` when not dry-running.

### workspace

```bash
monodev workspace ls
monodev workspace describe <workspace-id>
monodev workspace rm
monodev workspace rm <workspace-id>
monodev workspace rm --force
monodev workspace rm --dry-run
monodev workspace repair
monodev workspace repair --rebind <workspace-id>
monodev workspace repair --rebind <workspace-id> --force
```

`workspace rm` with no id deletes the current workspace (state file only; run
`unapply` first to remove overlays). `--force` (`-f`) deletes even if paths are
still applied. `repair` lists identity orphans; `--rebind` rewrites one onto the
current fingerprint. See [workspace-identity.md](workspace-identity.md).

---

## Store operations

### checkout

```bash
monodev checkout agent-context
monodev checkout -n agent-context
monodev checkout -n agent-context --description "agent files"
```

Select an existing store as active. `-n` / `--new` creates it. `--description`
is stored on create.

### track

```bash
monodev track path
monodev track Makefile .cursor scripts/dev
monodev track --agents
monodev track path --role script --description "helper" --origin user
```

Paths are resolved relative to the repo root. `--agents` tracks existing agent
context paths and reports absent ones. `--role` (`script`, `docs`, `style`,
`config`, `other`), `--description`, `--origin` (`user`, `agent`, `other`).

### untrack

```bash
monodev untrack path
```

Removes paths from `track.json` only. Does not modify workspace files or delete
store overlay bytes.

### commit

```bash
monodev commit path
monodev commit --all
monodev commit --all --dry-run
```

Copy tracked workspace files into the active store. Requires paths or `--all`.

### save

```bash
monodev save
monodev save --dry-run
```

Discover new files under tracked directories (skipping git-ignored paths),
track them, then commit everything. `--dry-run` previews both steps.

### store

```bash
monodev store ls
monodev store describe
monodev store describe <store-id>
monodev store update --description "details"
monodev store update <store-id> --description "details"
monodev store rm <store-id>
monodev store rm <store-id> --force
monodev store rm <store-id> --dry-run
```

`describe` / `update` without an id use the active store. `rm --force` (`-f`)
skips the in-use prompt. Deleting a store does not unapply workspace files.

---

## Remote persistence

Stores are pushed to a separate orphan branch (`monodev/persist` by default).
The branch is visible to anyone with repository access and is not encrypted.
Do not push secrets without a separate protection mechanism.

### init

```bash
monodev init
monodev init --force
```

Explicit initializer. `--force` (`-f`) reinitializes an existing `.monodev`.

### remote

```bash
monodev remote use origin
monodev remote show
monodev remote set-branch monodev/custom
monodev remote set-branch monodev/persist
```

Config lives at `.monodev/remote.json`. `use` verifies the git remote exists.

### push

```bash
monodev push
monodev push my-store
monodev push store1 store2
monodev push my-store --with-workspace
monodev push --with-workspace
monodev push my-store --dry-run
monodev push my-store --force
monodev push my-store --allow-secrets
monodev push my-store --remote origin
```

No store IDs: push all local stores, unless `--with-workspace` is set with no
IDs, which pushes only the current workspace reference. `--force` overwrites
remote. `--allow-secrets` pushes after a secret-scan finding. `--remote`
overrides the configured remote.

### pull

```bash
monodev pull
monodev pull my-store
monodev pull store1 store2
monodev pull my-store --force
monodev pull --workspace <remote-workspace-id> --with-stores
monodev pull my-store --remote origin
```

No IDs: pull every remote store. Checksums are always verified; a missing
manifest warns. Local content that differs is refused unless `--force`.
`--workspace` restores a persisted workspace reference into this checkout;
`--with-stores` also pulls stores named by that reference.

### sync

```bash
monodev sync
monodev sync --allow-secrets
```

Commit all tracked paths, push, then pull, in that order. Fails if no remote is
configured; run `monodev remote use origin` first.

---

## CLI tooling

```bash
monodev version
monodev completion bash
monodev completion zsh
monodev completion fish
monodev completion powershell
```

Workflows: [solo.md](solo.md), [team.md](team.md), [worktrees.md](worktrees.md).
