# @kjanat/actionlint

[![NPM Version](https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black)](https://npm.im/@kjanat/actionlint)

Static checker for GitHub Actions workflow files, distributed as a prebuilt binary.

This is the npm distribution of [`kjanat/actionlint`](https://github.com/kjanat/actionlint), a fork of
[rhysd/actionlint](https://github.com/rhysd/actionlint). Installing it puts an `actionlint` executable on your `PATH`; no
Go toolchain is needed.

## Install

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it without adding it to the project:

```sh
npx @kjanat/actionlint
```

## Usage

After installing it as a development dependency, run it in the project and it finds the workflows itself:

```sh
npx actionlint
```

As a package script:

```json
{
  "scripts": {
    "lint:workflows": "actionlint"
  }
}
```

`actionlint` exits `0` when it finds nothing, `1` when it reports problems, and `2` or `3` on a usage error or a fatal
error. Those statuses are forwarded verbatim, so it drops into CI unchanged.

See the [usage documentation](https://github.com/kjanat/actionlint/blob/master/docs/usage.md) for the full command line,
and [the checks list](https://github.com/kjanat/actionlint/blob/master/docs/checks.md) for what it looks for.

The manual page ships in the package as `man/actionlint.1`. npm registered man pages with the system `man` program up to
v11; from v12 it no longer does, so on a current npm read it directly:

```sh
man ./node_modules/@kjanat/actionlint/man/actionlint.1
```

### Configuration schema

The package includes the configuration schema. For editor completion and validation, use the versioned GitHub CDN URL:

```yaml
# yaml-language-server: $schema=https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json
```

ShellCheck directives and Ruff options reference their versioned definitions in
`schemas/shellcheck/` and `schemas/ruff/` relative to the main schema. The package
includes these files, so local schema validation can run offline. A versioned CDN
URL uses the schemas from the same repository version.

<details>
<summary>CDN URLs for the JSON schema</summary>

Choose the major version to follow its releases, or pin an exact release:

jsDelivr:

```text
https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json
```

Exact release:

```text
https://cdn.jsdelivr.net/gh/kjanat/actionlint@2.0.0/actionlint.schema.json
```

For v1:

```text
https://cdn.jsdelivr.net/gh/kjanat/actionlint@1/actionlint.schema.json
https://cdn.jsdelivr.net/gh/kjanat/actionlint@1.17.0/actionlint.schema.json
```

For example, use jsDelivr in an editor's schema directive:

```yaml
# yaml-language-server: $schema=https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json
```

Match the schema's major version to the installed actionlint version.

</details>

### ShellCheck

`actionlint` checks shell scripts inside `run:` steps with [ShellCheck][shellcheck]
when it is on your `PATH`. ShellCheck is not bundled here; install it separately to enable these checks.

## How this package is put together

This package contains no binary itself. It declares one `optionalDependencies` entry per platform, each published
under the `@kjanat-actionlint` scope so the binaries stay out of the `@kjanat` namespace:

| Package                                      | Runs on               |
| -------------------------------------------- | --------------------- |
| `@kjanat-actionlint/actionlint-darwin-arm64` | macOS Apple silicon   |
| `@kjanat-actionlint/actionlint-darwin-x64`   | macOS Intel           |
| `@kjanat-actionlint/actionlint-freebsd-ia32` | FreeBSD 32-bit x86    |
| `@kjanat-actionlint/actionlint-freebsd-x64`  | FreeBSD x86-64        |
| `@kjanat-actionlint/actionlint-linux-arm64`  | Linux ARM64           |
| `@kjanat-actionlint/actionlint-linux-arm`    | Linux ARMv6 and ARMv7 |
| `@kjanat-actionlint/actionlint-linux-ia32`   | Linux 32-bit x86      |
| `@kjanat-actionlint/actionlint-linux-x64`    | Linux x86-64          |
| `@kjanat-actionlint/actionlint-win32-arm64`  | Windows ARM64         |
| `@kjanat-actionlint/actionlint-win32-ia32`   | Windows 32-bit x86    |
| `@kjanat-actionlint/actionlint-win32-x64`    | Windows x86-64        |

Each declares `os` and `cpu`, so your package manager downloads only the one matching your machine and skips the rest.
The `actionlint` command here is a small launcher that resolves that package and execs the binary inside it.

There is no separate musl build: the binaries are statically linked, so the `linux-*` packages run on Alpine and on
glibc distributions alike.

The binaries are the same ones attached to the [GitHub release][releases]; each archive is verified against the
release's published checksums before being repackaged.

### If the binary is not found

The launcher fails with an explanation, but the usual cause is a package manager that skipped optional dependencies.
Reinstall without `--no-optional` or `--omit=optional`.

Using Bun with `minimumReleaseAge`? Add `@kjanat-actionlint/*` to `minimumReleaseAgeExcludes` alongside
`@kjanat/actionlint`. A fresh release otherwise installs the facade while its binaries are still age-gated.

## Analysis results

CLI `check --json` and the GitHub Action emit the same versioned result contract.
This package ships the schema and types supported by its matching binary:

```typescript
import type { CheckResult, DiagnosticRecord } from '@kjanat/actionlint/result';
```

`@kjanat/actionlint/result.schema.json` exports the result schema;
`@kjanat/actionlint/schemas/results/v1.schema.json` selects its contract version.
`CheckResult` describes a complete result, including failed analysis;
`DiagnosticRecord` describes one JSONL record. Types do not validate untrusted JSON.
See [the result contract](https://github.com/kjanat/actionlint/blob/master/docs/results.md)
for compatibility and migration details.

## Other ways to install

Homebrew, Arch (AUR), Scoop, Docker, a download script, and `go install` are all covered in
[the installation documentation](https://github.com/kjanat/actionlint/blob/master/docs/install.md).

## License

MIT. See [LICENSE](./LICENSE).

[releases]: https://github.com/kjanat/actionlint/releases
[shellcheck]: https://www.shellcheck.net/
