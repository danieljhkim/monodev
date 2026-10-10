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
stores win when they track the same path. A copied directory and a path inside
it from another store are both kept, in either order: the more specific path is
owned by its store, and the directory owner does not own that nested path.
Unapply of either store leaves the other store's files in place.

A path inside a file, or any nested overlap in symlink mode, is a conflict.
Apply stops before it changes the workspace, and `--force` does not override
that conflict. `--force` (`-f`) still overrides unmanaged destinations and mode
or type mismatches. `--dry-run` prints the plan and writes nothing.

### unapply

```bash
monodev unapply
monodev unapply store-a
monodev unapply --all
monodev unapply --force
monodev unapply --dry-run
```

No arguments: remove paths owned by the active store. Store IDs: remove only
those owners. Removing a copied directory leaves nested paths that another
applied store still owns, and drops those paths from the directory manifest so
the ledger matches the files left on disk. `--all` removes every applied overlay
in this workspace and cannot be combined with store IDs. `--force` (`-f`)
removes a drifted copy anyway, but still keeps another store's owned paths.
`--dry-run`.

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
When the changed region between two versions of a file is very large (millions of line pairs), `--patch` shows that region as a full delete-then-add replacement instead of a minimal diff, to bound memory use.
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

### context

```bash
monodev context
monodev context --json
```

Session-start entry point for agents. If this workspace has an active store,
reports it. Otherwise looks for a workspace reference for this directory
(matched by repository identity and workspace path, newest wins), first in
the local persistence work tree, then on the configured remote's persistence
branch. A match is pulled with its stores and applied through the normal
`pull` and `apply` checks: a local store that differs from the remote, or an
apply conflict, fails the command. `context` never passes `--force` and never
pushes.

It then lists the agent paths on disk, relative to the workspace:

- notes: files under `.agents/notes/`, such as `lessons.md`;
- sessions: files under `.agents/notes/sessions/`;
- scripts: files under `.agents/scripts/`.

No remote, offline, or nothing found exits 0 with an empty result and a
one-line hint.

`--json` prints one object. Every field is always present; lists are `[]`,
never `null`:

| Field | Type | Meaning |
| --- | --- | --- |
| `workspaceId` | string | This workspace's ID |
| `workspacePath` | string | Workspace path relative to the repository root |
| `source` | string | `active-store`, `local-reference`, `remote-reference` or `none` |
| `activeStore` | string | Active store, or `""` |
| `stores` | string[] | Applied stores in ledger order, then the active store |
| `pulledStores` | string[] | Stores pulled by this call |
| `notes` | string[] | Files under `.agents/notes/`, excluding sessions |
| `sessions` | string[] | Files under `.agents/notes/sessions/` |
| `scripts` | string[] | Files under `.agents/scripts/` |
| `warnings` | string[] | Non-fatal issues, such as a failed fetch |
| `hint` | string | One-line next step, or `""` |

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

Paths are resolved relative to the current workspace directory (the cwd), not
the repository root. A path that escapes that directory, or resolves to the
directory itself, is rejected. `--agents` tracks existing agent context paths
and reports absent ones. `--role` (`script`, `docs`, `style`,
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
monodev store clone <source> <destination>
monodev store rm <store-id>
monodev store rm <store-id> --force
monodev store rm <store-id> --dry-run
```

`describe` / `update` without an id use the active store. `rm --force` (`-f`)
skips the in-use prompt. Deleting a store does not unapply workspace files.
`clone` copies the source store's committed overlay and tracking metadata into
a new destination store with its own identity and independent contents. The
destination must not already exist; the command refuses without overwriting
it. Cloning does not activate the destination or change the workspace.

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

No store IDs: push this repository's stores (`<repo>/.monodev/stores/`), unless
`--with-workspace` is set with no IDs, which pushes only the current workspace
reference. Stores in `~/.monodev` or `MONODEV_ROOT` are shared across
repositories, so push them by name. `--force` overwrites
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

Commit all tracked paths from the active store for the current workspace, then
push and pull that same store, in that order. It does not synchronize other
local or remote stores. To push every repo-local store, use `monodev push`
with no store IDs; `monodev pull` with no IDs pulls every remote store. Fails if no remote is configured;
run `monodev remote use origin` first.

---

## CLI tooling

### skill

```bash
monodev skill init
monodev skill init --target claude
monodev skill init --target agents
monodev skill init --target all
monodev skill init --force
monodev skill show
```

`init` writes a `SKILL.md` that teaches coding agents the monodev loop:
session start with `context`, session end with a session file, `lessons.md`
and `save`, never `git add` agent files, restore with `apply`, check notes
against the code, the `unapply` and drift warnings, and what belongs in
committed docs instead. The content is generated from the binary's command
tree and stamped with its version.

Targets, verified against each tool's documentation:

- `claude`: `.claude/skills/monodev/SKILL.md`, where Claude Code loads project
  skills ([docs](https://code.claude.com/docs/en/skills)).
- `agents`: `.agents/skills/monodev/SKILL.md`, the
  [Agent Skills](https://agentskills.io/specification) layout Codex scans
  ([docs](https://learn.chatgpt.com/docs/build-skills)).

Without `--target`, `init` writes each target whose agent directory (`.claude/`
or `.agents/`) exists in this workspace, and errors if neither does. It needs
an active store (`monodev checkout -n <store>`): the skill is tracked in it,
snapshotted, and added to the managed exclude block, so `git status` stays
clean. A skill under an already-tracked directory (for example after
`track --agents`) is committed through that directory. `init` refuses to
overwrite an existing `SKILL.md` unless `--force` (`-f`); rerun with `--force`
after upgrading monodev. `show` prints the skill to stdout.

### Agent notes layout

Notes and scripts are plain files in a store overlay; monodev has no schema or
index for them. By convention:

- `.agents/notes/lessons.md`: short durable lessons, edited in place.
- `.agents/notes/sessions/<date>-<agent>-<id>.md`: one file per session, so
  parallel agents never write the same file and `pull` never hits a content
  conflict.
- `.agents/scripts/`: helper scripts.

Track the directories once (`monodev track .agents/notes .agents/scripts`);
`save` picks up new files under them. Decisions that bind everyone (ADRs) and
gotchas true for every contributor belong in reviewed, committed repo docs.

### Other tooling

```bash
monodev version
monodev completion bash
monodev completion zsh
monodev completion fish
monodev completion powershell
```

Workflows: [solo.md](solo.md), [team.md](team.md), [worktrees.md](worktrees.md).
