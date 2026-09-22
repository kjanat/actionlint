# bump-version

This is a script to update every release version reference in this repository.

The complete set of version-bearing files and fields is declared in [`targets.go`](./targets.go).
The Playground derives its version and links from Git during the Vite build, so its HTML is not a bump target.
The Nix package version in `flake.nix` is a bump target.
Each declaration names a file, a regular expression capturing the version, and the exact number of
occurrences expected in that file.

This script does:

- validate the given version and the state of the repository
- verify every [`CHANGELOG.md`](../../CHANGELOG.md) section has a matching heading, `[Changes]` link, and link definition
- resolve the release notes of the version and refuse to release without them
- verify each declared reference occurs exactly the expected number of times
- verify no version reference in a declared file is left undeclared
- rewrite every declared reference and verify the result on disk
- move the `Unreleased` entries of `CHANGELOG.md` into a dated section for the new version
- build and check the updated Nix package with the committed dependency lock, stopping on failure
- optionally commit and push the source changes, then dispatch draft release preparation

Nothing is written unless every file passes validation, and no commit, tag, or push happens unless
the rewritten repository is verified to reference the new version everywhere and the Nix checks pass.

## Prerequisites

- Go
- `git`
- GitHub CLI authenticated to this repository when dispatching preparation
- Node.js 24 or newer and Git configured for GPG signing when promoting a tested draft
- Nix with `nix-command` and `flakes` enabled, locally or in an installed WSL distribution

## Usage

```sh
go run ./scripts/bump-version [FLAGS] VERSION
```

Report every declared version reference without modifying anything, and fail if the references disagree on the release version.

```sh
go run ./scripts/bump-version -check
```

Update all references to 1.2.3. This modifies the files and leaves the changes in the working tree.

```sh
go run ./scripts/bump-version 1.2.3
```

Update all references and commit the source changes. This creates no release tag.

```sh
go run ./scripts/bump-version -commit 1.2.3
```

Update all references, commit and push `master`, then dispatch
[release preparation](../../.github/workflows/release-prepare.yml) for that source commit.
This builds the release candidate and tests it before creating a draft. It does not publish a release
or push a version tag.

```sh
go run ./scripts/bump-version -push 1.2.3
```

The same command works on Windows. The script uses Nix from `PATH` when available. Otherwise, on Windows,
it checks installed WSL distributions and selects the first one that can run Nix, including installations
loaded by the user's login profile. Go and Git keep running on Windows against the same checkout.
Use `-nix-command` only to override that automatic selection.

Detection runs before any files change. The script then runs
`nix flake check --no-update-lock-file --print-build-logs` after updating the version and changelog.
On failure, it leaves those updates for inspection without committing or tagging. Fix the failure and rerun the Nix
check before committing and dispatching preparation manually. A normal bump requires a clean checkout.

After reviewing and committing version edits manually, push the source commit and start preparation:

```sh
git push origin master
gh workflow run release-prepare.yml --ref master -f version=1.2.3
```

Preparation builds a child commit containing the complete source tree, root `action.mjs`, and
`SHA256SUMS`. The candidate commit travels in a Git bundle; its version tag exists only inside the
build job. The workflow builds archives and package manifests without publishing, tests the candidate
on Linux, macOS and Windows, checks Nix, and tests the container before uploading the draft assets.

After preparation succeeds, inspect its checks and draft assets, then promote that run from a clean
`master` checkout at the prepared source commit:

```sh
node scripts/release-candidate.mjs promote --version 1.2.3 --run RUN_ID
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
[Nix development](../../CONTRIBUTING.md#nix-development). `-check` and `-notes` do not require Nix.

Print the release notes of a version, which is what the release workflow publishes.

```sh
go run ./scripts/bump-version -notes v1.2.3
```

Bumping the version requires a clean working tree, a clean index, the `master` branch, a version tag
which does not exist yet, and release notes for the version. The notes are the `v1.2.3` section of
`CHANGELOG.md` when the file has one, and the `Unreleased` entries when it does not. The bump moves
the `Unreleased` entries into a `v1.2.3` section dated today, so the bump commit carries the
complete changelog and no post-release edit is needed. Writing the entries under `Unreleased` as
changes land is the only manual changelog work.

## Adding a version reference

When a new file or a new line starts referring to a release version, add it to `targets` in
[`targets.go`](./targets.go). Until it is declared, `-check` and `go test ./scripts/bump-version`
fail with the file and line of the undeclared reference.

Version numbers which are not actionlint release versions, such as the minimum pre-commit version
or an upstream specification version, are listed in the `unrelated` field of the target instead.
The script requires each of those literals to still be present, so a stale declaration is reported
rather than silently ignored.
