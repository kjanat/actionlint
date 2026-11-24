# npm packaging

This directory builds [`@kjanat/actionlint`](https://www.npmjs.com/package/@kjanat/actionlint).

```text
distribution/npm/
  targets.json         published platforms and the release asset for each
  targets.schema.json  schema for the above
  src/                 TypeScript package builder and CLI
  test/                Synthetic-archive and package-output tests
  facade/              facade template with launcher sources and unstamped manifest
  dist/                build output, gitignored
```

## Layout of the published packages

- `@kjanat/actionlint` holds the launcher, the manual and the configuration schema. It lists every platform package in `optionalDependencies` at the same version.
- `@kjanat-actionlint/actionlint-<os>-<cpu>` holds one executable and declares `os` and `cpu`. The eleven platform packages live in the `kjanat-actionlint` npm organisation.

The launcher resolves `actionlint-<process.platform>-<process.arch>` and starts its binary.

The facade exports the configuration schema as `./schema`. Published builds copy the schema from the release tag.

The binaries are static. One `linux-<cpu>` package serves glibc and musl hosts.

## Building locally

The builder downloads the GoReleaser archives of a release and checks each one against the release checksums.

```bash
INPUT_REPOSITORY=kjanat/actionlint \
  INPUT_VERSION=2.0.0 \
  node distribution/npm/src/cli.ts build
```

The command writes to `distribution/npm/dist/`. Set `INPUT_DOWNLOADS=/path/to/archives` to use downloaded archives. Set `INPUT_ONLY=linux-x64` or `INPUT_ONLY=facade` to build one package. `build --help` lists the flags.

## Tests

Test the launcher without platform packages:

```bash
bun run --filter @kjanat/actionlint test
```

Test the builder:

```bash
bun run --filter @kjanat/actionlint-npm test
```

Smoke-test the published package from a directory outside this checkout:

```sh
npx --yes @kjanat/actionlint@latest -version
```

Inside the checkout, `npx @kjanat/actionlint` can pick the `0.0.0` workspace template and report no platforms.

## Automation

`npm-release.yml` also runs on `workflow_dispatch`. Callers can pass `tag`, `dist-tag` and `dry-run`. Stable releases publish under `latest` and prereleases under `next`.

One job publishes each package, the platform packages first. The platform packages deploy from the `npm-actionlint` environment and the facade from `npm`. The jobs publish through OIDC without an `NPM_TOKEN`.

If publishing fails partway, rerun only the failed jobs. npm rejects a second upload of a published version.

The workflow enables [npm provenance][provenance] through `NPM_CONFIG_PROVENANCE`.
The `dry-run` input builds and smoke-tests without publishing and turns provenance off. A dry run uses the packaging sources of the workflow commit. Publishing uses the packaging sources of the release tag.

An account with write access and 2FA manages the bindings:

```sh
npm trust list PACKAGE
npm trust github PACKAGE --repository=kjanat/actionlint --file=WORKFLOW.yml --environment=ENVIRONMENT --allow-publish --yes
npm trust revoke PACKAGE --id=TRUST_ID
```

After you rename a workflow, add the new binding and revoke the old one.

## Adding a platform

1. Add the `goos`/`goarch` pair to the relevant `builds` entry in `.goreleaser.yaml`.
2. Add a target to `targets.json`. `pkg` must be `<os>-<cpu>` in Node's `process.platform` and `process.arch` spellings. `asset` must match the `GOOS_GOARCH` pair in the GoReleaser asset name. ARM builds carry the variant, as in `linux_armv6`.
3. Add it to the table in `facade/README.md`.

The tests fail when a target's name and its `os`/`cpu` disagree.

[provenance]: https://docs.npmjs.com/generating-provenance-statements
