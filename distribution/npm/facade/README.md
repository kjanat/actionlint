# @kjanat/actionlint

[![NPM Version](https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black)](https://npm.im/@kjanat/actionlint)
[![pkg.pr.new](https://pkg.pr.new/badge/kjanat/actionlint)](https://pkg.pr.new/~/kjanat/actionlint)

Check GitHub Actions workflows, action manifests and embedded scripts with
[`kjanat/actionlint`](https://github.com/kjanat/actionlint), a fork of
[rhysd/actionlint](https://github.com/rhysd/actionlint). This package installs the
`actionlint` command without requiring Go.

## Install

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it without adding it to the project:

```sh
npx @kjanat/actionlint
```

## Usage

Run this command from your Git repository to check its workflows and actions:

```sh
npx actionlint check
```

As a package script:

```json
{
  "scripts": {
    "lint:workflows": "actionlint check"
  }
}
```

`actionlint` exits `0` when it finds nothing, `1` when it reports problems, and `2` or `3` on a usage error or a fatal
error. CI can use these exit codes to fail a check.

See the [usage documentation](https://github.com/kjanat/actionlint/blob/master/docs/usage.md) for the full command line,
and [the checks list](https://github.com/kjanat/actionlint/blob/master/docs/checks.md) for what it looks for.

If your system has the `man` command, read the included manual with:

```sh
man ./node_modules/@kjanat/actionlint/man/actionlint.1
```

### Pre-commit hooks

With [Husky](https://typicode.github.io/husky/get-started.html), check the whole repository before each commit:

```sh
npm install --save-dev husky
npx husky init
```

Set `.husky/pre-commit` to:

```sh
npm run lint:workflows
```

This uses the package script above and checks working-tree files, including unstaged changes.

To check only staged workflows and action manifests, install
[lint-staged](https://github.com/lint-staged/lint-staged):

```sh
npm install --save-dev lint-staged
```

Add to `package.json`:

```json
{
  "lint-staged": {
    ".github/workflows/*.{yml,yaml}": "actionlint check --",
    "**/action.{yml,yaml}": "actionlint check --"
  }
}
```

Replace `.husky/pre-commit` with:

```sh
npx lint-staged
```

This does not recheck every workflow that calls a changed action. Run the full check in CI too.

### Configuration schema

Use the npm package's schema through jsDelivr:

```yaml
# yaml-language-server: $schema=https://cdn.jsdelivr.net/npm/@kjanat/actionlint@2/actionlint.schema.json
```

Match the installed major version (`@1` or `@2`), pin a release (`@1.17.0`), or omit the version to follow the latest release.

### Shell and Python checks

For a new project, install the default ShellCheck and Ruff plugins:

```sh
npx actionlint config init
```

These plugins check scripts in workflow steps without requiring native ShellCheck,
Ruff or Python executables. For an existing configuration, follow the
[configuration migration guide](https://github.com/kjanat/actionlint/blob/master/docs/config-migration.md).

## Preview builds

Run a pull request preview, say PR 343, without installing it in your project:

```sh
npx https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@343 check
# Or use a commit SHA:
npx 'https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@<commit-sha>' check
# Or use a branch name:
npx 'https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@<branch-name>' check
```

Replace `343` with the pull request number or commit SHA.

## Supported platforms

Your package manager installs the binary for your operating system and CPU:

| Operating system | CPU architectures                       |
| ---------------- | --------------------------------------- |
| macOS            | Apple silicon, Intel x64                |
| Linux            | x64, ARM64, 32-bit x86, ARMv6 and ARMv7 |
| Windows          | x64, ARM64, 32-bit x86                  |
| FreeBSD          | x64, 32-bit x86                         |

Linux builds support both glibc and musl, including Alpine Linux.

## Troubleshooting

### If the binary is not found

The launcher fails with an explanation, but the usual cause is a package manager that skipped optional dependencies.
Reinstall without `--no-optional` or `--omit=optional`.

## JSON output

To read findings from another program:

```sh
npx actionlint check --json
```

TypeScript types are included:

```typescript
import type { CheckResult, DiagnosticRecord } from '@kjanat/actionlint/result';
```

`CheckResult` describes JSON output; `DiagnosticRecord` describes one JSONL record.
For runtime validation, use `@kjanat/actionlint/result.schema.json`.
See the [output fields](https://github.com/kjanat/actionlint/blob/master/docs/results.md).

## Other ways to install

Homebrew, Arch (AUR), Scoop, Docker, a download script, and `go install` are all covered in
[the installation documentation](https://github.com/kjanat/actionlint/blob/master/docs/install.md).

## License

MIT. See [LICENSE.txt](./LICENSE.txt).
