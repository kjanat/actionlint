# Prepare release versions

`node tools/release/prepare-version.mjs VERSION` writes the release version to [`VERSION`](../../VERSION) and updates the changelog.

The command rejects a version older than the one in `VERSION`, an existing release tag, and a missing changelog entry or release-notes file. It moves the Unreleased entries into a dated version section unless that section exists. It then runs `nix flake check --no-update-lock-file --print-build-logs`. On failure it leaves the edits uncommitted for inspection.

## Prerequisites

- GitHub CLI authenticated to this repository when dispatching preparation
- Node.js 24 or newer
- Git configured for GPG signing when promoting a tested draft
- Nix with `nix-command` and `flakes` enabled, locally or in an installed WSL distribution

## Usage

```sh
node tools/release/prepare-version.mjs [OPTIONS] VERSION
```

Validate the current version, changelog and release notes without changing anything.

```sh
node tools/release/prepare-version.mjs --check
```

Set the version and leave the changes in the working tree:

```sh
node tools/release/prepare-version.mjs 1.2.3
```

Set the version and commit. `--commit` needs a clean working tree and index.

```sh
node tools/release/prepare-version.mjs --commit 1.2.3
```

Set the version, commit, push `master` and dispatch [release preparation](../../.github/workflows/release-prepare.yml). `--push` needs the `master` branch.

```sh
node tools/release/prepare-version.mjs --push 1.2.3
```

On Windows, the script uses Nix from `PATH` or the first installed WSL distribution that can run it. Select another launcher with `--nix-program PROGRAM` and one `--nix-arg=ARG` per argument.

After you commit version edits by hand, push and start preparation:

```sh
git push origin master
gh workflow run release-prepare.yml --ref master -f version=1.2.3
```

The Nix check does not refresh `flake.lock` or `vendorHash`. Update them as described in [Nix development](../../CONTRIBUTING.md#nix-development).

## Preparation

A reported vulnerability stops preparation before it creates the draft.

If preparation fails, fix the source, delete any failed draft and dispatch again.

## Promotion

Inspect the run's checks and draft assets. Then promote the run from a clean `master` checkout at the prepared source commit:

```sh
node tools/release/release-candidate.mjs promote --version 1.2.3 --run RUN_ID
```

Promotion verifies the run, its manifest and every asset checksum. It signs `v1.2.3` at the candidate, records an `ours` merge, pushes `master` and the tag atomically, and publishes the draft. If publication fails after signing or pushing, rerun the same command.

Never tag the source-only `HEAD` by hand. It lacks the generated `action.mjs`.
