#!/usr/bin/env bash
# Builds monodev and a throwaway repo for docs/assets/demo.tape.
# Run from the repository root: vhs docs/assets/demo.tape
set -euo pipefail

demo_root="${TMPDIR:-/tmp}/monodev-demo"
rm -rf "$demo_root"
mkdir -p "$demo_root/bin" "$demo_root/acme"
go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o "$demo_root/bin/monodev" ./cmd/monodev

cd "$demo_root/acme"
git init -q -b main
printf '# acme\n' > README.md
git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm init
mkdir -p .claude && printf '{}\n' > .claude/settings.json
printf '# Agent notes\n' > AGENTS.md
printf 'Prefer small diffs.\n' > .cursorrules
printf 'print("debug")\n' > debug_helper.py
