# Prepare release versions

Run `node tools/release/prepare-version.mjs VERSION` to write the release version to [`VERSION`](../../VERSION) and update the changelog before preparing a release draft.

`VERSION` holds the release version as one `MAJOR.MINOR.PATCH` line. `flake.nix` reads the Nix package version from it.
The manual filter [`man/release-version.lua`](../../man/release-version.lua) reads it for the manual footer and
rewrites links to `/blob/HEAD/docs/` so they point at the release tag.
The Playground derives its version and links from Git during the Vite build.
Version numbers in documentation examples are fixed examples. A release leaves them unchanged.

Before changing files, the command validates the version, repository state,
[`CHANGELOG.md`](../../CHANGELOG.md) sections and release-notes file. It rejects a version older than the one in `VERSION`.

It then updates `VERSION` and the changelog and runs Nix with the committed dependency lock.
`--commit` creates a source commit after Nix succeeds. `--push` also pushes `master` and dispatches
draft preparation. Tag creation and release publication belong to the later promotion command.

## Prerequisites

- `git`
- GitHub CLI authenticated to this repository when dispatching preparation
- Node.js 24 or newer
- Git configured for GPG signing when promoting a tested draft
- Nix with `nix-command` and `flakes` enabled, locally or in an installed WSL distribution

## Usage

```sh
node tools/release/prepare-version.mjs [OPTIONS] VERSION
```

Report the version in `VERSION` without modifying anything, and fail if the changelog or the release-notes file for that version is invalid.

```sh
node tools/release/prepare-version.mjs --check
```

The same check is available as `mise run release:version -- --check`.

Set the version to 1.2.3. This modifies `VERSION` and `CHANGELOG.md` and leaves the changes in the working tree.

```sh
node tools/release/prepare-version.mjs 1.2.3
```

Set the version and commit the source changes. This creates no release tag.

```sh
node tools/release/prepare-version.mjs --commit 1.2.3
```

Set the version, commit and push `master`, then dispatch
[release preparation](../../.github/workflows/release-prepare.yml) for that source commit.
This builds the release candidate and tests it before creating a draft. It does not publish a release
or push a version tag.

```sh
node tools/release/prepare-version.mjs --push 1.2.3
```

The same command works on Windows. The script uses Nix from `PATH` when available. Otherwise, on Windows,
it checks installed WSL distributions and selects the first one that can run Nix, including installations
loaded by the user's login profile. Node.js and Git run on Windows against the same checkout.
To select a launcher explicitly, use `--nix-program PROGRAM` and repeat `--nix-arg=ARG` for its arguments.

Detection runs before any files change. The script then runs
`nix flake check --no-update-lock-file --print-build-logs` after updating the version and changelog.
On failure, it leaves those updates for inspection without committing or tagging. Fix the failure and rerun the Nix
check before committing and dispatching preparation manually. Automatic commits require a clean checkout; local edit preparation can run with unrelated changes.

After reviewing and committing version edits manually, push the source commit and start preparation:

```sh
git push origin master
gh workflow run release-prepare.yml --ref master -f version=1.2.3
```

Preparation builds a child commit containing the complete source tree, root `action.mjs`,
and `SHA256SUMS`. Pre-commit hooks are released from their dedicated repository.
The candidate commit travels in a Git bundle; its version tag exists only inside the
build job. The workflow builds archives and package manifests without publishing, tests the candidate
on Linux, macOS and Windows, checks Nix, and tests both CLI and compatibility Action containers on
Linux amd64 and arm64 before uploading the draft assets. Container tests load the stored OCI archives;
the manifest includes both archives and their image digests.

Preparation checks the candidate's Go packages with govulncheck and scans both architectures of each
container image with OSV Scanner. A reported vulnerability stops preparation before a draft is created.
Container builds update the installed Alpine packages without reusing the cached runtime stage.

Publication copies those archives with digest preservation; it never rebuilds the images. The CLI
image uses version tags. The compatibility image wraps the same ordinary binary with
the shipped nine positional inputs and uses the GHCR tags `action-X.Y.Z`, `action-vX.Y`, `action-vX`,
and `action-latest`. Moving tags advance only when the release is latest for their scope.

After publication, the runner resolves literal external `owner/repo@vX.Y.Z` and `owner/repo@COMMIT`
references on Linux, macOS, Windows and `ubuntu-slim`, downloading the published binary for each run.
These checks gate moving Action tags. Preparation's local archive smoke cannot prove external runner
resolution or published downloads; a successful preparation alone does not establish those checks.

After preparation succeeds, inspect its checks and draft assets, then promote that run from a clean
`master` checkout at the prepared source commit:

```sh
node tools/release/release-candidate.mjs promote --version 1.2.3 --run RUN_ID
```

Promotion checks the successful run, its immutable manifest artifact, every draft asset checksum,
and the candidate's relationship to the source commit. It signs the normal `v1.2.3` tag at that
exact candidate, records its ancestry with an `ours` merge, atomically pushes `master` and the tag,
then publishes the existing draft. It does not rebuild assets. The merge preserves the source tree
while making the version tag reachable by `git describe`.

Do not manually tag the source-only `HEAD`: it has no generated `action.mjs`. Normal `vX.Y.Z` tags
serve both CLI and Action users; eligible moving `v1` and `v1.2` tags advance after distribution checks.
No GPG private key is uploaded to CI. If publication fails after signing or pushing, rerun the same
promotion command; it accepts the matching signed tag and recorded merge. If preparation fails, fix
the source and dispatch again. An existing draft is not silently replaced; inspect and remove the
failed draft before preparing that version again. Keep the successful run's manifest artifact until
promotion; an expired artifact requires new preparation.

The check does not refresh `flake.lock` or `vendorHash`. Update Nixpkgs deliberately with `nix flake update nixpkgs`,
and update the Go dependency hash when dependencies change, as described in
[Nix development](../../CONTRIBUTING.md#nix-development). Checking versions and printing release notes do not require Nix.

Print the release notes published by the release workflow:

```sh
node tools/release/release-candidate.mjs notes --version v1.2.3
```

The release tag must not exist locally or on origin. Preparation requires changelog entries and the version's release-notes file. It uses the existing version section when present; otherwise it moves the Unreleased entries into a section dated in Europe/Amsterdam. Automatic commits require a clean working tree and index. Pushing also requires the master branch. The version check requires the release-notes file for the version in `VERSION`.
