# monodev

Coding agents write files into your repo that do not belong in git: `.claude/`,
`.cursorrules`, `AGENTS.md`, scratch scripts, local env files. They either show
up in `git status` and get committed, or they get deleted and you lose the
context.

Those files get a third place. A **store** holds the overlay. After you apply
it, the files are on disk for you and the agent, and git cannot see them.

## Before / after

Agent context sitting in a checkout:

```text
$ git status
On branch main
Untracked files:
  (use "git add <file>..." to include in what will be committed)
	.claude/
	.cursorrules
	AGENTS.md
	debug_helper.py

nothing added to commit but untracked files present (use "git add" to track)
```

Capture it, then apply:

```bash
monodev checkout -n agent-context
monodev track --agents
monodev track debug_helper.py
monodev commit --all
monodev apply
```

Git is clean. The files are still there:

```text
$ git status
On branch main
nothing to commit, working tree clean

$ ls -d .claude .cursorrules AGENTS.md debug_helper.py
.claude
.cursorrules
AGENTS.md
debug_helper.py
```

Apply copies the overlay into the working tree and writes a managed block in
`.git/info/exclude`. Unapply removes the copies. The store keeps them.

- **Invisible**: `git status` stays clean.
- **Persistent**: the overlay survives branch switches and fresh checkouts.
- **Portable**: push and pull stores on a separate orphan branch, not on `main`.

![monodev preview](docs/assets/cli_preview.png)

## Stores and workspaces

A **store** is the named snapshot: which paths are overlaid, and their contents.
New stores land in `<repo>/.monodev/stores/` (auto-created on first use, with a
`*` gitignore so the directory itself stays out of git). Existing stores under
`~/.monodev/stores/` remain visible. Set `MONODEV_ROOT=$HOME/.monodev` to keep
using the home-directory root instead of creating `.monodev` in the repo.

A **workspace** is one directory in one checkout: which store is active, and
which overlays are currently applied. Workspace IDs hash a repo fingerprint plus
the relative path from the repo root. Changing directory inside the repo is a
different workspace. See [docs/workspace-identity.md](docs/workspace-identity.md).

Stores are *what*. Workspaces are *where* and *whether it is applied*.

## Quick start

Supported on macOS and Linux; Windows is not. See [Platform support](#platform-support).

```bash
# Install
brew install danieljhkim/tap/monodev

# Or download the archive matching your OS and CPU from GitHub Releases,
# verify it with SHA256SUMS, and place the contained monodev binary on PATH.

monodev checkout -n agent-context
monodev track --agents
monodev commit --all
monodev apply
git status
```

`--agents` tracks the agent-context paths that exist (`.claude/`, `CLAUDE.md`,
`.cursor/`, `.cursorrules`, `AGENTS.md`, `.codex/`, `.gemini/`, and friends) and
skips the ones that do not.

Downloaded release archives also contain shell completions under `completions/`
and man pages under `man/`. After extracting an archive:

```bash
PREFIX="${PREFIX:-$HOME/.local}"
DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}"
mkdir -p "$PREFIX/bin" "$DATA_DIR/bash-completion/completions" \
  "$HOME/.zfunc" "$HOME/.config/fish/completions" "$DATA_DIR/man/man1"
install -m 0755 monodev "$PREFIX/bin/monodev"
install -m 0644 completions/monodev.bash \
  "$DATA_DIR/bash-completion/completions/monodev"
install -m 0644 completions/_monodev "$HOME/.zfunc/_monodev"
install -m 0644 completions/monodev.fish "$HOME/.config/fish/completions/monodev.fish"
install -m 0644 man/monodev.1 "$DATA_DIR/man/man1/monodev.1"
```

Add `fpath=(~/.zfunc $fpath)` to `.zshrc` before `compinit` for zsh, and add
`${XDG_DATA_HOME:-$HOME/.local/share}/man` to `MANPATH` if your system does not
already search it. The Homebrew formula installs these same files automatically.

## Everyday commands

```bash
monodev status
monodev store ls
monodev checkout agent-context
monodev track path
monodev save
monodev commit --all
monodev diff
monodev apply
monodev unapply
monodev apply store-a store-b
monodev unapply --all
monodev doctor
monodev doctor --fix
```

`save` is the session closer once you have tracked directories: it finds new
files under those directories, tracks them, and commits everything. `commit --all`
writes the currently tracked paths and does not discover new ones.

`apply` with no arguments applies the active store. Extra store IDs apply in
order; later stores win path conflicts. `unapply` without arguments removes the
active store's overlay; `unapply --all` removes every applied overlay in this
workspace.

Full flag list: [docs/commands.md](docs/commands.md).

## Workflows

- [Solo developer in a monorepo](docs/solo.md)
- [Team sharing through the persistence branch](docs/team.md)
- [Parallel agents in git worktrees](docs/worktrees.md)

Leave a workspace without deleting stores: `monodev eject` (keeps the files;
`eject --remove-files` deletes the overlays). Interrupted apply/unapply/eject
recovers through the overlay journal; see
[docs/overlay-recovery.md](docs/overlay-recovery.md).

## Platform support

**Supported:** macOS (Apple Silicon and Intel) and Linux (AMD64 and ARM64).
Tagged releases publish a versioned `tar.gz` for each of those targets plus a
`SHA256SUMS` file. The release workflow runs `monodev version` on each target's
native runner; Linux targets also run the filesystem and real-Git integration
suite.

**Not supported:** Windows.

Two independent reasons, not neglect:

1. **POSIX advisory locking.** Mutating commands take `flock(2)` locks under
   each state root's `.locks/` directory (`golang.org/x/sys/unix`). The binary
   does not build on Windows.
2. **Symlink overlay mode.** The overlay planner still has a symlink apply path
   (legacy workspace mode). Windows symlink creation needs Developer Mode or
   elevation and has different semantics; that path is not supported.

Shipped `apply` currently copies files rather than linking them. A copy-mode-only
Windows build is a **plausible future** after locking is ported off `flock(2)`
(for example to `LockFileEx`). It is **not planned**: there is no Windows CI,
no Windows release artifact, and a copy-only change without portable locking
still would not compile. Treat Windows as out of scope unless that locking work
lands first.

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

## Status

Early development.

Built it for personal use, but contributions and design feedbacks are welcomed.

## License

MIT
