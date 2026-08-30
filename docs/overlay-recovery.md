# Overlay transaction recovery

Apply and unapply share one recoverable transaction. Filesystem changes and
the workspace ownership ledger are kept aligned across operation failure,
cancellation, and process restart. Journals written by the retired
`stack apply` / `stack unapply` commands still recover through this path.

## Journal

Each mutating operation writes a journal beside workspace state:

- `<workspaces>/<workspace-id>.txn.json` — durable intent and phase
- `<workspaces>/<workspace-id>.txn/` — backups of overwritten destination trees
  and staged replacements

Dry-run never writes a journal and never mutates the workspace.

## Phases

1. **preparing** — existing destinations are copied into the txn directory and
   incoming trees are staged there. Live workspace paths are still original.
2. **prepared** — backups and stages are complete. Destinations may then be
   swapped. A crash in this window is rolled back from backups.
3. **committed** — destinations match the intended result. The journal stores
   the final workspace state (or a delete). Only after this phase is durable
   are backups removed.

File copies write a sibling temp and rename over the destination. They never
truncate the live file in place. Directory replacements are staged completely
before the live tree is moved aside.

Overwritten user content is kept in the txn backup directory until the
committed journal and workspace state save have succeeded.

## Restart path

The next mutating apply or unapply acquires the workspace lock, then recovers
any journal before planning:

| Phase | Recovery |
| --- | --- |
| `preparing` | Discard the txn directory. Destinations were not mutated. |
| `prepared` | Restore each destination from its backup (or delete a destination that did not exist before). Idempotent if some swaps already ran. |
| `committed` | Retry `SaveWorkspace` or `DeleteWorkspace`, then discard the txn directory. |

Retrying the original command after recovery is idempotent: a rolled-back
workspace is applied or unapplied from the restored ledger, and a committed
workspace already matches the intended result.

Cancellation during install attempts rollback. If rollback fails, the journal
remains so the next invocation can finish recovery. Overlay mutations are never
left untracked.
