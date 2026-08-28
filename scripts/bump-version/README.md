# bump-version

This is a script to update every release version reference in this repository.

The complete set of version-bearing files and fields is declared in [`targets.go`](./targets.go).
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
- optionally create the version bump commit, the version tag, and push them

Nothing is written unless every file passes validation, and no commit, tag, or push happens unless
the rewritten repository is verified to reference the new version everywhere.

## Prerequisites

- Go
- `git`

## Usage

```sh
go run ./scripts/bump-version [FLAGS] VERSION
```

Report every declared version reference without modifying anything.

```sh
go run ./scripts/bump-version -check
```

Update all references to 1.2.3. This modifies the files and leaves the changes in the working tree.

```sh
go run ./scripts/bump-version 1.2.3
```

Update all references, then create the bump commit and the `v1.2.3` tag locally.

```sh
go run ./scripts/bump-version -commit 1.2.3
```

Update all references, create the bump commit and the tag, and push both to `origin`. Pushing the
tag starts [the release workflow](../../.github/workflows/release.yaml).

```sh
go run ./scripts/bump-version -push 1.2.3
```

Print the release notes of a version, which is what the release workflow publishes.

```sh
go run ./scripts/bump-version -notes v1.2.3
```

Bumping the version requires a clean working tree, a clean index, the `main` branch, a version tag
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
