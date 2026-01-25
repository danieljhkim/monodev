# monodev

I frequently work with a giant monorepo consisting of countless components nested at varying depths. Because of its scale, it has a noticeable memory footprint on my machine and imposes subotimal landscape for AI agents. 

To work around this, I selectively open individual components as isolated IDE workspaces, manually excluding irrelevant directories via settings.json. And during developing, I would additionally add various component-specific artifacts such as .cursor, Makefile, AGENTS.md, run.py, etc. 

Long story short, these dev/component specific artifacts cannot easily be commited and persisted methodically, making their reusability difficult across different branches and sessions.

So I created `monodev`.

`monodev` is a local-only CLI for managing **reusable development overlays** (scripts, editor config, agent-instructions, Makefiles, etc.) across large monorepos.

It lets you:
- keep dev-only files out of git
- persist them safely per component or profile
- re-apply and remove them deterministically

---

## CLI Preview

![CLI Preview](docs/assets/cli_preview.png)

---

## Core ideas

- **Stores**  
  Named overlay sets stored outside the repo (`~/.monodev/stores`).

- **Stacking**  
  Multiple stores can be composed:
  ```
  global → profile → component
  ```
  Later stores override earlier ones.

- **Modes**
  - `symlink` (default): store is source of truth
  - `copy`: workspace is detached; `save` syncs back

- **Safety**
  - Explicit apply / unapply
  - No implicit deletes
  - Conflict detection + `--force` escape hatch

---

## Example workflow

Basic create, save & apply:

```bash
# create and select a store
monodev use -n services/search/indexer

# track dev-only files
monodev track Makefile .cursor scripts/dev .claude .vscode

# persist workspace → store
monodev save --all

# removes the saved artifacts from current workspace
monodev unapply

# you can also add global-scoped stores like below:
monodev use -n python --scope global
monodev track .gitignore .venv requirements.txt 
monodev save --all

# later, in another repo:
monodev use python
# this will add those artifacts to the current dir
monodev apply
# when you are done, simply unapply:
monodev unapply
```

Applying multiple stores at once:

```bash
# if you want to apply multiple existing stores, add to stack
monodev stack add python
monodev stack add services/search/indexer

# applies python + services/search/indexer store overlays
monodev apply

# removes them all
monodev unapply
```

---

## Commands

### Core

- `monodev use <store-id>`  
  Select an existing store as the active store.

- `monodev use -n <store-id> [--scope global|component] [--description "some details"]`  
  Create a new store and set it as the active store.

### Stack management

- `monodev stack ls`  
  List stores in the current stack (in order).

- `monodev stack add <store-id>`  
  Add a store to the end of the stack.

- `monodev stack pop [<store-id>]`  
  Remove a store from the stack. If no ID is provided, pop the last store.

- `monodev stack clear`  
  Remove all stores from the stack.

### Tracking & persistence

- `monodev track <path>...`  
  Track one or more paths in the active store (metadata only).

- `monodev untrack <path>...`  
  Stop tracking one or more paths in the active store.

- `monodev save [<path>...]`  
  Persist workspace content into the active store.

- `monodev save --all`  
  Persist all tracked paths into the active store.

- `monodev prune`  
  Remove store overlay content for paths that are no longer tracked.

### Workspace mutation

- `monodev apply`  
  Apply the store stack (plus active store) to the current directory.

- `monodev unapply`  
  Remove previously applied overlays from the current directory.

### Inspection

- `monodev status`  
  Show the current workspace status and applied overlays.

- `monodev list`  
  List available stores.

- `monodev describe <store-id>`  
  Show detailed metadata and tracked paths for a store.

---

## What monodev is (and isn’t)

**Is**
- per-workspace dev overlay manager
- designed for monorepos
- deterministic

**Is not**
- a build system
- a dependency manager
- a replacement for dotfiles or Nix
- always reversible

---

## Status

Early development. 

Built it for personal use, but contributions and design feedbacks are welcomed.

## License

MIT