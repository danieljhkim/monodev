# Releasing monodev

This runbook describes the repository's current release path. `main` is both
the integration branch and the branch from which releases are cut.

## Versioning

The release version comes from the annotated Git tag. The Makefile obtains it
with `git describe --tags --always --dirty` and passes it to the Go binary as
`main.version`; there is no committed Cargo, npm, or other package version to
bump for a monodev release.

monodev is currently pre-1.0, using `0.<minor>.<patch>`:

- Non-breaking changes propose a patch bump, such as `0.2.8` to `0.2.9`.
- Breaking changes propose a minor bump, such as `0.2.8` to `0.3.0`.

Removing or changing a documented command, flag, persisted-data contract, or
other supported user-facing behavior is a breaking-change candidate. The human
release owner confirms the final classification and version; the release-prep
probe and release task must not make that decision autonomously.

## Prepare the release

1. Confirm the release commit is on `main` and the working tree is clean.
2. Find the latest reachable release tag and survey only the non-merge commits
   after it:

   ```sh
   git tag --merged HEAD --list 'v*' --sort=-version:refname | head -n 1
   git log v<previous>..HEAD --pretty='%h%x09%s' --no-merges
   git log v<previous>..HEAD --pretty='%s' --no-merges \
     | grep -oE 'DANI-[0-9]+' | sort -u
   ```

   If no reachable `v*` tag exists, stop and establish a human-reviewed
   baseline instead of inferring one. The survey is a no-merge release view,
   not a request to merge or publish anything.
3. Review the referenced tasks and commit subjects for user-visible changes and
   breaking-change candidates. Propose the semver bump under the policy above,
   then obtain the human release owner's confirmation of the classification.
4. Compile the consumer-facing changes into a new dated `## [X.Y.Z]` section
   directly below `## [Unreleased]` in `CHANGELOG.md`. Preserve released
   sections as history; do not turn the changelog into a commit-by-commit log.
   Keep the existing category style and include every confirmed breaking change
   with an actionable migration or replacement where one exists.
5. Create or update the single `Prepare v<X.Y.Z> release` chore, tagged
   `release`, for the approved release. It must point to this runbook and use
   only `file:CHANGELOG.md` as a repository modification target: the binary
   version is tag-derived, not stored in a committed manifest.

## Verify before the tag

Run the repository checks used by CI:

```sh
make lint
make test
make test-integration
```

The release workflow additionally builds four target archives: darwin/arm64,
darwin/amd64, linux/amd64, and linux/arm64. For a non-publishing workflow
exercise, use **Run workflow** with the intended version and `publish=false`.
The workflow builds with `make release-artifacts`, smokes `monodev version`,
checks Bash/Zsh/Fish completions and the man page, produces one archive and
sidecar checksum per target, verifies all four checksums, and assembles
`SHA256SUMS`. Review those generated artifacts before approval to publish.

## Approve, tag, and publish

The human release owner must approve the changelog, version classification,
and release task before any commit, tag, push, or publication. After approval:

1. Commit the approved `CHANGELOG.md` change on `main` and ensure the three
   checks above remain green.
2. Create an annotated tag on that commit:

   ```sh
   git tag -a v<X.Y.Z> -m 'v<X.Y.Z>'
   ```

3. Push the `main` commit first, then the tag:

   ```sh
   git push origin main
   git push origin v<X.Y.Z>
   ```

Pushing the tag starts `.github/workflows/release.yml`. It creates the GitHub
Release with the four archives and `SHA256SUMS`, then updates
`danieljhkim/homebrew-tap` with the darwin/arm64 archive URL, checksum, version,
completions, and man page. Confirm both the GitHub Release and Homebrew tap
update completed successfully.

## Failure and hotfix recovery

Do not force-push, move, or reuse a published release tag. First inspect the
failed workflow and its artifacts to identify whether the failure is build,
documentation, archive/checksum, GitHub Release, or Homebrew publication. A
workflow-dispatch run with `publish=false` may be used to diagnose build and
artifact failures without creating a release.

For a code, changelog, or packaging correction, prepare a new human-approved
hotfix on `main`, choose the next appropriate tag under the pre-1.0 policy, and
repeat the verification and push order above. If assets or Homebrew publication
failed after the GitHub Release was created, preserve the original tag and
release evidence; coordinate the repair with the human release owner rather
than silently replacing published artifacts.
