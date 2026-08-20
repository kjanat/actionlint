# bump-version

This is a script to update every release version reference in this repository.

The complete set of version-bearing files and fields is declared in [`targets.go`](./targets.go).
Each declaration names a file, a regular expression capturing the version, and the exact number of
occurrences expected in that file.

This script does:

- validate the given version and the state of the repository
- verify each declared reference occurs exactly the expected number of times
- verify no version reference in a declared file is left undeclared
- rewrite every declared reference and verify the result on disk
- optionally create the version bump commit, the version tag, and push them

Nothing is written unless every file passes validation, and no commit, tag, or push happens unless
the rewritten repository is verified to reference the new version everywhere.

## Prerequisites

- Go
- `git`

## Usage

```
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

Bumping the version requires a clean working tree, a clean index, the `main` branch, and a version
tag which does not exist yet.

## Adding a version reference

When a new file or a new line starts referring to a release version, add it to `targets` in
[`targets.go`](./targets.go). Until it is declared, `-check` and `go test ./scripts/bump-version`
fail with the file and line of the undeclared reference.

Version numbers which are not actionlint release versions, such as the minimum pre-commit version
or an upstream specification version, are listed in the `unrelated` field of the target instead.
The script requires each of those literals to still be present, so a stale declaration is reported
rather than silently ignored.
