# monodev

Most codebases suffer from "local file drift." We generate debug scripts, AI scratchpads (.cursorrules, .claude, etc.), and task notes that live alongside our code but don't belong in the repo. These files are either accidentally committed (clutter) or deleted too soon (lost knowledge).

`monodev` introduces a third space: **Local-First Overlays**. It keeps your dev-only artifacts persistent and portable without ever leaking them into your Git history.

The Monodev Way:
- **Invisible**: Keeps "git status" clean.
- **Persistent**: Your notes, scripts, and agent files survive branch switches.
- **Portable**: Push/Pull your local state via hidden orphan branches.

---

## Quick Start

Platform support: macOS (Apple Silicon only)

```bash
# 1. Install
brew install danieljhkim/tap/monodev

# 2. Create your first store and track a file
monodev checkout -n my-debug-tools
monodev track debug_helper.py
monodev commit --all

# 3. Remove the overlay after done working
monodev unapply

# 4. Reapply again later when needed
monodev apply

monodev help
```

![monodev preview](docs/assets/cli_preview.png)

---

## Core ideas

### **Stores**
A **store** is a named, reusable snapshot of **dev-only files** (editor config, scripts, agent instructions, Makefiles, etc.).

- A store defines *what* files are overlaid and their contents
- Stored at: `~/.monodev/stores/<store-id>/`

You can think of a store as a portable bundle of development artifacts that can be applied across multiple components or sessions.

### **Workspaces**
A **workspace** represents a specific directory within a repository where overlays are applied.

- Each workspace tracks:
  - the active store
  - which stores are currently applied
- Workspace IDs are derived from:
  - the repository fingerprint (hashed git remote URL)
  - the relative path within the repo
- Stored at: `.monodev/workspaces/<workspace-id>.json`

> **In short:** stores define *what* dev artifacts exist, and workspaces define *where* and *when* they are applied.

---

## Basic workflow

### Basic create, track, commit & apply:

```bash
# create and check out a store (similar to `git checkout` but doesn't apply overlays yet)
# this also sets the store as "active store" for the current directory
monodev checkout -n my-component-store 

# track dev-only files for the "active store" (similar to `git add`)
monodev track Makefile .cursor scripts/dev .claude .vscode

# check status of the current workspace
monodev status

# persist the tracked files to the store (similar to `git commit`)
monodev commit --all

# check for modified tracked files
monodev diff
# if you want to commit the changes, you can do:
monodev commit --all

# removes the "active store" overlays from the current directory
monodev unapply

# later, in another component directory:
monodev checkout my-component-store
monodev apply # this will add those artifacts to the current dir
monodev unapply # this will remove the overlays from the current dir
```

### How it works

When you invoke `monodev checkout <store-id>` under a specific directory within a repo, a workspace file is created in `.monodev/workspaces/<workspace-id>.json`. This file contains the metadata for the workspace, including the active store, the applied stores, and the tracked paths.

The `workspace-id` is derived from the repo fingerprint (hashed git remote URL + absolute path) and the relative path to the workspace. So when you cd into to a different directory, you will not have an "active store" for that directory. And when you cd back to the original component directory, the active store is restored. 

When you invoke `monodev apply` with the active store, the overlays are applied to the current directory. This is done by creating copies of the tracked paths to the current directory.

You can use `monodev status` to see the current workspace status and applied overlays.

![monodev status](docs/assets/monodev_status.png)

---

## Commands

### Core commands

These are the core commands you will use most often. You can still apply multiple store overlays using these commands multiple times. 

When there are conflicts (i.e. multiple stores claim the same path), you can use `--force` to override them. When conflicts are overridden, your latest actions (unapply, apply) will take precedence.

```bash
# this shows the current workspace status and applied overlays
monodev status

# this lists all available stores
monodev store ls

# this shows the detailed metadata and tracked paths for a store
monodev store describe <store-id>

# this deletes a store and all its overlay artifacts
monodev store rm <store-id>

# this sets the active store (store must already exist)
monodev checkout <store-id>

# this creates a new store and sets it as the active store
monodev checkout -n <store-id> [--description "some details"] [--type "issue | plan | feature | task | other"] [--priority "low | medium | high | none"]

# this tracks a path in the active store (.monodev/<store-id>/track.json is updated)
monodev track <path>

# this untracks a path in the active store (.monodev/<store-id>/track.json is updated)
monodev untrack <path>

# update the active store metadata
monodev store update <store-id> [--status "todo | in_progress | done | blocked | cancelled | other"]

# persist the tracked paths in the active store (.monodev/<store-id>/overlay is updated)
monodev commit <path>

# persist all tracked paths in the active store
monodev commit --all

# this applies the "active store's" overlays to the current workspace
monodev apply [--force] [--dry-run]

# this removes the "active store's" applied overlays from the current workspace
monodev unapply [--force] [--dry-run]

```

### Workspace management

```bash
# list all workspaces
monodev workspace ls

# show detailed information about a workspace
monodev workspace describe <workspace-id>

# delete a workspace
monodev workspace rm <workspace-id>
```

### Stack management

To easily manage multiple stores in one go, you can use the stack command. 

Stack isn't technically required (you can still use `monodev apply/unapply` multiple times), but it's a convenient way to manage multiple stores. 

When using stack, the "active store" is not affected - use `monodev apply/unapply` separately for that.

When there are conflicts (i.e. multiple stores claim the same path), you can use `--force` to override them - later stores take precedence.

```bash
# list all stores in the stack
monodev stack ls

# add a store to the stack
monodev stack add <store-id>

# remove a store from the stack
monodev stack pop [<store-id>]

# clear the stack
monodev stack clear

# apply the stack to the current workspace
monodev stack apply [--force] [--dry-run]

# remove the stack-applied overlays from the current workspace
monodev stack unapply [--force] [--dry-run]
```

### Remote persistence

Share stores across machines and teams using Git-based remote persistence. Stores are pushed to a separate orphan branch (`monodev/persist` by default) to keep them isolated from your main repository history.

```bash
monodev init # initialize the .monodev directory in the repository root

# Configure which Git remote to use for persistence
monodev remote use origin

# Show current remote configuration
monodev remote show

# Set a custom persistence branch (optional)
monodev remote set-branch monodev/custom

# Push existing stores to remote
monodev push <store-id>...

# Pull stores from remote
monodev pull <store-id>...

# Pull and verify checksums
monodev pull <store-id>... --verify

# Force pull (overwrite local stores)
monodev pull <store-id>... --force
```

**How it works:**

1. Remote configuration is stored locally at `.monodev/remote.json`
2. Stores are materialized to `.monodev/persist/stores/` before pushing
3. A separate Git repository is created at `.monodev/.git` with an orphan branch
4. The orphan branch is pushed to your configured remote
5. When pulling, stores are fetched and dematerialized to `~/.monodev/stores/`

This approach keeps persistence separate from your main Git history while leveraging Git's compression and deduplication.

---

## What monodev is (and isn't)

**Is**
- per-workspace dev overlay manager
- designed for monorepos and large codebases
- deterministic
- portable

**Is not**
- a build system
- a dependency manager
- a replacement for dotfiles or Nix

---

## Status

Early development. 

Built it for personal use, but contributions and design feedbacks are welcomed.

## License

MIT