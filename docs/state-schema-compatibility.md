# State schema compatibility

Monodev treats persisted state as user data, not cache. Every JSON document
that carries `schemaVersion` follows this policy: store `meta.json` and
`track.json`, workspace state, pushed workspace references, repo-local remote
configuration, overlay transaction journals, and store verification manifests.

## Version promise

A `schemaVersion` bump is a durable compatibility boundary. A newer monodev
binary reads every older supported version, applies its documented migration,
and writes the current version. Migrations are idempotent: repeating one
produces the same current document. They change only fields they own and keep
unrecognized top-level fields from the old document intact.

An older binary must never partially decode a file from a newer binary. It
first reads only `schemaVersion`, then refuses a higher version with the file
path, found and supported versions, and the action to upgrade monodev. This is
especially important for a persistence branch shared by machines running
different releases: a pull fails before it changes local state.

Missing `schemaVersion` is the legacy version zero. It remains readable when
the format's migration supports it. New writes always include the current
version. A format may retire a field only in a versioned migration; it cannot
silently reinterpret that field.

## Current migrations

Workspace state version 2 retires the old `stack` field. Its migration derives
`appliedStores` from existing ownership records, removes `stack`, and preserves
any unrelated top-level fields through later state saves. Overlay transaction
journals version 2 rename the old `version` header to `schemaVersion` without
changing recovery data.

## Operator action

When a compatibility error names a higher `schemaVersion`, install and rerun
with a newer monodev release. Do not edit the file to lower its version: the
newer writer may rely on fields the older binary cannot safely interpret.
