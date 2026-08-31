# Publishing verified SSH Fleet Console releases

## Version model

- Development builds use `<branch>-<12-char-commit>`, for example
  `dev-b941263e4144`; `+dirty` proves that tracked or untracked source changes
  were present during the build.
- Stable builds use strict SemVer tags `vMAJOR.MINOR.PATCH`. The first planned
  public release is `v0.1.0`.
- Before `v1.0.0`, MINOR may change product behavior and PATCH is reserved for
  compatible bug fixes and security hardening. From `v1.0.0`, normal SemVer
  compatibility rules apply.
- Stable versions only increase. Tags/releases are immutable and a given
  version is never rebuilt from another commit.
- The binary embeds version, channel, branch, full commit, clean/dirty state and
  commit timestamp. `sshfleet version --json` makes this provenance verifiable after
  the binary leaves Git.

`make test-version` is the executable version contract. It covers human/JSON
output, clean/dirty/unknown provenance, rejected unsafe linker metadata, strict
and increasing SemVer, main/SHA/worktree/origin gates, deterministic release
archives, VERSION checksums, Linux arm64 packaging and the protected
verify-to-publish workflow topology. The same suite is an explicit step of
`make regression`, so its log and status remain in regression evidence.

## Repository model

- This repository is `sshfleet-console`; it never publishes SSH Fleet Web or
  SSH Fleet Hub artifacts.
- `dev` is the default integration branch and the target for ordinary pull
  requests.
- `main` contains only release-ready commits promoted from `dev` by reviewed
  pull request.
- Direct pushes and manual tags on `main` are forbidden. A stable tag is created
  by the Release workflow only after its gate succeeds.
- `sshfleet-console-*` release archives contain the one-binary core for Linux amd64/arm64, a VERSION
  manifest, canonical `sshfleet` launcher, transitional `sshf` launcher,
  documentation and SHA-256 checksums. The installer creates `sf` only when the
  name is unused and never replaces an unrelated command.

The owner selected Apache-2.0 on 2026-08-31. The repository and every core
release archive carry `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.md` and exact
preserved dependency license texts. `make test-licenses` derives the production
module graph from `./cmd/sshf` and rejects missing, stale or modified notices.
The complete clean-history and settings checklist is in
[publishing.md](publishing.md).

## One-time GitHub setup

1. Create the repository and add it as `origin`; push `main` and `dev`, then set
   `dev` as the default branch.
2. Protect both branches. Require `CI / core`, forbid direct pushes to `main`,
   require a reviewed `dev → main` pull request, require conversation resolution
   and dismiss stale approvals after new commits.
3. Create a protected GitHub Actions environment named `release`, require a
   maintainer approval, prevent self-review and disable administrator bypass.
   Only `main` may deploy to it.
4. Keep the repository-wide Actions token read-only. Build and tests run in the
   read-only `verify` job. Only the approved `publish` job receives
   `contents: write`; it executes no repository code.
5. Enable immutable releases, private vulnerability reporting, Dependabot,
   secret scanning and signed commits where available.

## What “verified release” guarantees

The workflow runs on one immutable `GITHUB_SHA` and refuses anything except the
current clean `origin/main`. Before a tag exists it performs:

1. increasing strict SemVer and duplicate-tag checks;
2. full `make regression`: unit, race, vet, source/security, real PTY/menu,
   installer, full toolchain and disposable Docker acceptance;
3. public secret/history audit;
4. deterministic amd64/arm64 builds with embedded commit provenance;
5. native binary/version verification, VERSION manifests and SHA-256 checks;
6. upload of the candidate and regression evidence retained independently from
   the release.

Only after all six stages succeed and a maintainer approves the protected
environment does a separate minimal job download those exact bytes, recheck
checksums and current `main`, create a draft with all assets, then publish the
release. This guarantees the published commit passed the documented gate; it is
not a claim that software can contain no undiscovered defects.

## Cutting a release

```sh
git switch dev
make regression
make audit-public

# Merge a reviewed dev -> main PR. Do not create a tag manually.
```

In GitHub Actions select **Release → Run workflow**, choose branch `main`, and
enter the next version, initially `v0.1.0`. The workflow itself creates the tag
and publishes archives, regression evidence and checksums. A run from another
branch, dirty/outdated main, duplicate/lower version, failed test or mismatched
commit stops before publication.

For a local development archive that is clearly not stable:

```sh
make release-snapshot
./bin/sshfleet version
```

## Private milestone tags

`archive/pre-tabs` at `b74656c` and `archive/tabs` at the current verified
terminal-tabs snapshot are local/private navigation markers. They are not
SemVer releases, carry no support promise and must never be pushed to the future
public remote because their reachable development history contains private
identifiers. Public stable history starts clean; its first tag remains `v0.1.0`
and is created only by the protected workflow.

## Companion projects

Official upstream gitlinks remain in this repository because they pin audited
source revisions without embedding source or binaries in an ordinary clone.
Core CI and release builds do not initialize them until the full regression asks
for them. Create a fork only when SSH Fleet Console must carry a source patch; otherwise
an extra fork adds maintenance and trust cost without benefit.
