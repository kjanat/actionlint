# npm packaging

Sources for the npm distribution. The published package is
[`@kjanat/actionlint`](https://www.npmjs.com/package/@kjanat/actionlint).

```
npm/
  targets.json         the platforms published, and the release asset each is cut from
  targets.schema.json  schema for the above
  facade/              the published facade package: launcher sources and manifest
  dist/                build output, gitignored
```

## Layout of the published packages

One facade plus one package per platform, the usual shape for shipping a native
binary through npm:

- `@kjanat/actionlint` contains no binary. It declares every platform package in
  `optionalDependencies`, pinned to the exact same version, and its `bin` entry
  is a small launcher.
- `@kjanat/actionlint-<os>-<cpu>` contains one executable and declares `os` and
  `cpu`, so a package manager downloads only the one matching the host.

At run time the launcher derives the platform package name as
`<facade>-<process.platform>-<process.arch>`, resolves it, and execs the binary
inside. The name is derived rather than searched for, so resolution never
depends on the order `optionalDependencies` happens to be written in — which is
why `targets.json` requires every `pkg` to equal `<os>-<cpu>`, and why both the
builder and the launcher tests assert it.

There is deliberately no libc dimension. The release binaries are built with
`CGO_ENABLED=0` and are statically linked, so one `linux-<cpu>` package serves
glibc and musl hosts alike. The equivalent layout for a Rust binary has to
detect the host libc and choose between a gnu and a musl build; this does not.

## Where the binaries come from

They are not rebuilt. `.github/actions/npm-packages` downloads the GoReleaser
archives already attached to the GitHub release, checks each one against the
release's own published checksums manifest, and unpacks the executable. What
npm serves is byte-for-byte what the release attested, and a truncated download
or a swapped asset fails the build instead of reaching the registry.

## Building locally

```bash
cd .github/actions/npm-packages
GITHUB_WORKSPACE="$(git rev-parse --show-toplevel)" \
  INPUT_VERSION=1.13.0 \
  go run .
```

That fetches the release archives for the given version and writes the tree to
`npm/dist/`. Add `INPUT_DOWNLOADS=/path/to/archives` to use already-downloaded
assets, and `INPUT_ONLY=linux-x64` (or `facade`) to build just one package.

## Tests

The launcher's resolution logic, including every failure path, is covered
without needing any platform package installed:

```bash
npm run test:npm-facade
```

The builder has its own tests, which assemble synthetic release archives, run
the whole pipeline, and assert each package ends up with its own binary:

```bash
cd .github/actions/npm-packages && go test ./...
```

## Automation

`.github/workflows/npm-release.yaml` publishes on every `release: published`
event, and on a manual `workflow_dispatch`. It builds the tree, smoke-tests the
launcher against a real binary, then publishes the platform packages **before**
the facade — publishing the facade first would leave a window in which
installing it cannot resolve a binary.

Releases are published with [npm provenance][provenance], and a semver
prerelease tag goes out under the `next` dist-tag rather than `latest`.

Use the workflow's `dry-run` input to build and smoke-test without publishing.

## Adding a platform

1. Add the `goos`/`goarch` pair to the relevant `builds` entry in
   `.goreleaser.yaml`, so the release actually carries the archive.
2. Add a target to `targets.json`. `pkg` must be `<os>-<cpu>` using Node's
   `process.platform` and `process.arch` spellings, and `asset` must match the
   `GOOS_GOARCH` pair GoReleaser puts in the asset name — note that ARM builds
   carry the variant, as in `linux_armv6`.
3. Mention it in the table in `facade/README.md`.

The tests fail if a target's name and its `os`/`cpu` disagree, which is the
mistake that would otherwise publish a package the launcher can never find.

[provenance]: https://docs.npmjs.com/generating-provenance-statements
