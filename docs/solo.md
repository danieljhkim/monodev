# Solo developer in a monorepo

Keep agent context, component Makefiles, and debug scripts on disk without
leaking them into `git status`. One person, one clone, maybe many packages.

First command that needs a state root creates `<repo>/.monodev/` with a `*`
gitignore. `monodev init` is the explicit form of that, including `--force`
to reinitialize.

## Capture agent context at the repo root

```bash
monodev checkout -n agent-context --description "agent files"
monodev track --agents
monodev track debug_helper.py
monodev commit --all
monodev apply
git status
```

`git status` is clean. `ls` still shows `.claude/`, `AGENTS.md`, and the other
tracked paths. `monodev status` lists them as applied.

## A package-local overlay

Workspaces are the directory you run from. A store created under `packages/api`
is a different workspace from the repo root:

```bash
cd packages/api
monodev checkout -n api-dev
monodev track Makefile .vscode
monodev commit --all
monodev apply
cd ../..
monodev status
```

Root status does not show `api-dev` as applied. `cd packages/api` and
`monodev status` does. Apply and unapply stay inside that workspace.

## After a working session

Once directories are tracked, `save` finds new files under them and commits:

```bash
monodev save --dry-run
monodev save
monodev diff
monodev status
```

`commit --all` writes currently tracked paths only. Use `track` (or `save`)
before `commit` when you added new paths.

## Take the overlay off, put it back

```bash
monodev unapply
monodev apply
```

`unapply` with no arguments removes the active store's overlay. Re-apply by
id if the workspace no longer has an active store:

```bash
monodev apply agent-context
```

Several stores in one directory:

```bash
monodev apply agent-context api-dev
monodev status
monodev unapply api-dev
monodev unapply --all
```

Later stores win path conflicts. `--force` overrides unmanaged destinations.

## Inspect and repair

```bash
monodev store ls
monodev store describe agent-context
monodev doctor
monodev doctor --fix
```

## Stop managing this workspace

```bash
monodev eject --dry-run
monodev eject --yes
```

Default eject keeps the files and drops the ledger and exclude block. Stores
remain; delete one with `monodev store rm` if you mean to.

```bash
monodev eject --remove-files --dry-run
```

Flags and extras: [commands.md](commands.md). Sharing with a teammate:
[team.md](team.md). Parallel agents: [worktrees.md](worktrees.md).
