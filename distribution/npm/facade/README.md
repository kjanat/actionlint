# @kjanat/actionlint

[![NPM Version](https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black)](https://npm.im/@kjanat/actionlint)

This package installs the `actionlint` command of [`kjanat/actionlint`](https://github.com/kjanat/actionlint), a fork of [rhysd/actionlint](https://github.com/rhysd/actionlint).

## Install

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it once:

```sh
npx @kjanat/actionlint
```

## Usage

Check your repository:

```sh
npx actionlint check
```

See the [usage documentation](https://github.com/kjanat/actionlint/blob/master/docs/usage.md) and [the checks list](https://github.com/kjanat/actionlint/blob/master/docs/checks.md). Read the manual:

```sh
man ./node_modules/@kjanat/actionlint/man/actionlint.1
```

### Pre-commit hooks

Add to `package.json`:

```json
{
  "lint-staged": {
    ".github/workflows/*.{yml,yaml}": "actionlint check --",
    "**/action.{yml,yaml}": "actionlint check --"
  }
}
```

Run the full check in CI too.

### Shell and Python checks

```sh
npx actionlint config init
```

For an existing configuration, follow the [configuration migration guide](https://github.com/kjanat/actionlint/blob/master/docs/config-migration.md).

## Preview builds

```sh
npx https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@master check
# Or use a commit SHA:
npx 'https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@<commit-sha>' check
# Or use a branch name:
npx 'https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@<branch-name>' check
```

A pull request number also works. Previews support macOS, Linux and Windows on x64 and ARM64.

## Supported platforms

| Operating system | CPU architectures                       |
| ---------------- | --------------------------------------- |
| macOS            | Apple silicon, Intel x64                |
| Linux            | x64, ARM64, 32-bit x86, ARMv6 and ARMv7 |
| Windows          | x64, ARM64, 32-bit x86                  |
| FreeBSD          | x64, 32-bit x86                         |

Linux builds support glibc and musl.

If the binary is not found, reinstall without `--no-optional` or `--omit=optional`.

## JSON output

```sh
npx actionlint check --json
```

The package includes TypeScript types:

```typescript
import type { CheckResult, DiagnosticRecord } from '@kjanat/actionlint/result';
```

Validate at run time with `@kjanat/actionlint/result.schema.json`. See the [output fields](https://github.com/kjanat/actionlint/blob/master/docs/results.md).
