# Documents

[![Latest release](https://img.shields.io/github/v/release/kjanat/actionlint?logo=github&logoColor=white&label=release&labelColor=181717&color=black)](https://github.com/kjanat/actionlint/releases)
[![NPM Version](https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black)](https://npm.im/@kjanat/actionlint)
[![Docker Image Version](https://img.shields.io/docker/v/kjanat/actionlint?logo=docker&logoColor=white&label=Docker&labelColor=2496ED&color=black)](https://hub.docker.com/r/kjanat/actionlint)

- [Checks](checks.md): All checks performed by actionlint, with examples, output, and playground links.
- [Installation](install.md): Install with npm, Homebrew, AUR, Scoop, WinGet, mise, a release archive, the download script,
  Docker, or Go. Includes upstream-only package names.
- [Usage](usage.md): How to use the `actionlint` command locally or on GitHub Actions, the online playground, an official Docker
  image, and integrations with reviewdog, Problem Matchers, super-linter, [native Git hooks](usage.md#native-git-hooks),
  and pre-commit.
- [Configuration](config.md): Runner labels, variables, secrets, default permissions, error filters, and opt-in policy
  checks, with YAML Language Server schema support.
- [Integration interfaces](api.md): Connect other programs through the CLI, language server and plugin SDKs.
- [Go dependency forks](go-forks.md): Dependency graph, fork origins and consuming modules.
- [Language server](lsp.md): Feature support, inference and editor integration.
- [CI security sources](design/ci-security-sources.md): Source-backed rule research and coverage limits.
- [Plugins](plugins.md): Write, build, test and load WebAssembly component plugins, and where built-in rules live.
- [v2 migration](migration-v2.md): Move pre-commit configurations to the separate hook repository.
- [References](reference.md): Links to resources.
- [GitHub Actions changelog](https://actionlint.kjanat.dev/github-changelog/): Browse and search the latest entries from
  GitHub's Actions changelog feed.
- [Repository layout](../repository-layout.md): Where actionlint reads inputs, builds workflow models, runs checks and writes results.
- [Historical package layout](design/package-layout.md): Dependency measurements and an earlier proposal for splitting the root Go package.
