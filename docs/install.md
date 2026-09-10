# Installation

[![GitHub Release][release-badge]][releases]

This document describes how to install the [kjanat/actionlint fork](../README.md). Package names matter: several
registries also provide [rhysd/actionlint][upstream] under the unqualified name `actionlint`.

ShellCheck and pyflakes are optional external linters. The standalone actionlint binary uses them when available on
`PATH`; see [external linter configuration](usage.md#ignore-some-errors).

## Project-maintained distributions

These methods install packages, binaries, or source published by this project.

### Windows

#### [Scoop](https://scoop.sh/)

[![Scoop Version][scoop-badge]][scoop-bucket]

This fork is available in the [kjanat bucket][scoop-bucket]:

```powershell
scoop bucket add kjanat https://github.com/kjanat/scoop-bucket
scoop install kjanat/actionlint
```

The [`actionlint` package in Scoop's main bucket][scoop] installs the upstream project.

### Linux

#### [Arch Linux](https://archlinux.org/)

[![AUR Version (git)][aur-git-badge]][actionlint-kjanat-git]
[![AUR Version (binary)][aur-bin-badge]][actionlint-kjanat-bin]
[![AUR Version (source)][aur-source-badge]][actionlint-kjanat]

Three packages for this fork are available in the [AUR][aur]:

| Package                                          | Installs                                      |
| ------------------------------------------------ | --------------------------------------------- |
| [`actionlint-kjanat-bin`][actionlint-kjanat-bin] | The prebuilt binary from a stable release     |
| [`actionlint-kjanat`][actionlint-kjanat]         | A stable release built from source            |
| [`actionlint-kjanat-git`][actionlint-kjanat-git] | The current `master` branch built from source |

Choose one and install it with an AUR helper such as [`paru`][paru]. For the prebuilt binary:

```sh
paru -S actionlint-kjanat-bin
```

All three install the manpage and shell completions. They conflict with one another and with the upstream
`actionlint`, `actionlint-bin`, and `actionlint-git` packages because they provide the same executable.

<a id="homebrew"></a>

### [Homebrew][homebrew] on macOS and Linux

Install this fork from the `kjanat/tap` tap:

```sh
brew install --cask kjanat/tap/actionlint
```

The unqualified [`actionlint` formula][formula] in Homebrew core installs the upstream project.

The per-project `kjanat/actionlint` tap redirects to `kjanat/tap` through Homebrew's tap migration metadata, so existing
`kjanat/actionlint/actionlint` installations keep upgrading. Releases publish only to `kjanat/tap`.

The cask has no package dependencies. [ShellCheck integration](checks.md#check-shellcheck-integ) is optional and uses
`shellcheck` when it is available on `PATH`; installing actionlint does not install ShellCheck. To add it separately,
the `kjanat/tap/shellcheck` cask provides the upstream static binary and shell completions with no dependencies.
Uninstall the homebrew-core formula first if it already owns the `shellcheck` binary:

```sh
brew install --cask kjanat/tap/shellcheck
```

> [!WARNING]
> The macOS executable is not notarized. If Gatekeeper blocks a downloaded copy, review and allow it in
> **System Settings → Privacy & Security**.

### [npm](https://www.npmjs.com/)

[![NPM Version][npm-badge]][npm-package]

[`@kjanat/actionlint`][npm-package] is available on the public npm registry. It installs a prebuilt binary for your
platform; no Go toolchain is needed:

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it without adding it to the project:

```sh
npx @kjanat/actionlint
```

The distribution uses platform packages containing the GitHub release binaries. Keep npm's optional
dependencies enabled, since the launcher needs the package matching your operating system and architecture.
Linux binaries are statically linked and work with both musl and glibc.

### Prebuilt binaries

Download an archive file from [the releases page][releases] for your platform, unarchive it and put the executable file to a
directory in `$PATH`.

Release archives are available for:

- macOS (x86_64, arm64)
- Linux (i386, x86_64, ARMv6-compatible 32-bit ARM, arm64)
- Windows (i386, x86_64, arm64)
- FreeBSD (i386, x86_64)

The release matrix builds all these targets, but native CI tests do not cover:

- Linux i386, 32-bit ARM
- Windows i386
- FreeBSD i386, x86_64

To install these binaries [`gh`][gh] command is useful. The following command is an example for x86_64 Linux.

```sh
gh release download --repo kjanat/actionlint --pattern '*_linux_amd64.tar.gz' v1.16.1
tar xf actionlint_1.16.1_linux_amd64.tar.gz
./actionlint -version
```

Verify the downloaded archive's [build provenance attestation][attestations] against this repository:

```sh
gh attestation verify -R kjanat/actionlint actionlint_1.16.1_linux_amd64.tar.gz
```

<a id="download-script"></a>

### Download script

To install `actionlint` executable with one command, [the download script](../scripts/download-actionlint.bash) is available.
It downloads `actionlint.exe` on Windows and `actionlint` on other supported platforms. Pass `latest` to resolve the
newest release, or omit the argument to use the default version recorded in the script:

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) latest
```

When you need to install specific version of actionlint, please give the version to the 1st command line argument. The following
example installs v1.16.1.

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) 1.16.1
```

This script downloads `actionlint` (or `actionlint.exe` on Windows) binary to the current working directory. When you need to put
the downloaded binary to some other directory, please give the directory path to the 2nd command line argument. The following
example installs the latest version to `~/.local/bin`. The destination must already exist and be on your `PATH`:

```sh
mkdir -p "$HOME/.local/bin"
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) latest "$HOME/.local/bin"
```

The script verifies the archive's attestation when an authenticated `gh` command is available; otherwise it reports
that attestation verification was skipped. It extracts only the executable, so use the complete release archive if you
also want the manpage and documentation.

For the usage of actionlint on GitHub Actions, see [the usage document](usage.md#on-github-actions).

### Docker image

[![Docker Image Version][docker-badge]][dockerhub]

The fork publishes CLI images as `ghcr.io/kjanat/actionlint` and `docker.io/kjanat/actionlint`. See
[Docker usage](usage.md#docker) for running the linter with a mounted repository.

### Cross-platform version managers

#### mise

Use [mise's GitHub backend][mise-github] with this repository's full name. By default, the short `actionlint` tool name resolves to
upstream packages in mise's registry.

```bash
# Show all installable versions (excluding minimum-release-age and pre-releases)
mise ls-remote github:kjanat/actionlint

# Install the latest release
mise install github:kjanat/actionlint@latest

# Set a version globally (on your ~/.config/mise/config.toml file)
mise use -g github:kjanat/actionlint@latest

# and simply run:
actionlint # or more explicitly: mise exec github:kjanat/actionlint@latest -- actionlint -version
```

For a project-local selection, put this in `mise.toml` and run `mise install`:

```toml
[tools]
"github:kjanat/actionlint" = "latest"
```

To make the short `actionlint` name resolve to this fork, add a [tool alias](https://mise.jdx.dev/dev-tools/aliases.html):

```sh
mise alias add actionlint github:kjanat/actionlint
mise use -g actionlint@1.16.1
```

`mise alias add` (also available as `mise tool-alias set`) writes the alias to `~/.config/mise/config.toml`.
The second command installs and selects the version globally.

To share the alias and version with a project instead, put both in its `mise.toml` and run `mise install`:

```toml
#:schema https://mise.jdx.dev/schema/mise.json

[tools]
actionlint = "1.16.1"

[tool_alias]
actionlint = "github:kjanat/actionlint"
```

With the alias configured, commands such as `mise install actionlint` and `mise ls-remote actionlint` use this fork's
GitHub releases. To list the `1.16` releases with metadata:

```sh
mise ls-remote --minimum-release-age 0 --json actionlint 1.16 | jq .
# `--minimum-release-age 0` includes releases newer than the default 24h.
# 1.16 filters by prefix.
```

<details><summary>console output</summary>

```json
[
  {
    "version": "1.16.0",
    "created_at": "2026-09-08T16:30:53Z",
    "release_url": "https://github.com/kjanat/actionlint/releases/tag/v1.16.0",
    "prerelease": false
  },
  {
    "version": "1.16.1",
    "created_at": "2026-09-09T19:28:57Z",
    "release_url": "https://github.com/kjanat/actionlint/releases/tag/v1.16.1",
    "prerelease": false
  }
]
```

</details>

### [Nix](https://nix.dev/)

The project's flake builds this fork from source. With Nix's `nix-command` and `flakes` features enabled, run it
without installing it into your profile:

```sh
nix run github:kjanat/actionlint -- --help
```

Or install it into your profile:

```sh
nix profile add github:kjanat/actionlint
```

The package includes ShellCheck and Pyflakes, the manpage, Bash/Zsh/Fish completions, and the configuration schema
at `share/actionlint/actionlint.schema.json`. The package version is updated by the release bump script.
The unversioned commands above build the default branch. Use a release tag or commit in the flake reference to
select a specific checkout, or keep this flake as a locked input in your own project. Release tags created before
the flake was added do not provide it.

The flake exposes packages for x86-64 and ARM64 Linux, and Apple silicon macOS. See [Nix development](../CONTRIBUTING.md#nix-development) for local
builds, checks, and the development shell. The separate [Nixpkgs package proposal](#nixpkgs) is still pending.

### Build from source

[![Go Module Version][go-module-badge]][go-module]

Use a [Go][Go] toolchain compatible with the revision's [`go.mod`](../go.mod). Its `go` directive specifies the minimum
Go version and its `toolchain` directive specifies the preferred toolchain; Go's automatic toolchain selection may
download a newer compiler. No Go toolchain is needed when using prebuilt binaries.

```sh
# Install the latest stable version
go install actionlint.kjanat.dev/cmd/actionlint@latest

# Install the head of the master branch
go install actionlint.kjanat.dev/cmd/actionlint@master
```

## Community-maintained integrations

### Python (pip and uv)

[![PyPI Version][pypi-badge]][pypi-package]

[`actionlint-py-kjanat`][pypi-package] is a community Python wrapper for this fork, maintained by
[René Fritze (@renefritze)][python-wrapper]. His [migration PR][python-wrapper-pr] switched the wrapper to this fork's
release binaries and gave it a separate PyPI package name.

Install it in your Python environment:

```sh
python -m pip install actionlint-py-kjanat
actionlint --version
```

Or run it in an isolated environment with [uv][uv-tools]:

```sh
uvx --from actionlint-py-kjanat actionlint
```

Installation downloads the binary for your platform from this repository's GitHub releases and verifies its SHA-256
checksum. It needs access to GitHub as well as PyPI; no Go toolchain is required.

The wrapper has its own release schedule and may package an older actionlint release. Its version includes an extra
wrapper revision; `actionlint --version` reports the binary's version. The original `actionlint-py` package wraps
the upstream project.

## Pending packages

### [Winget](https://learn.microsoft.com/en-us/windows/package-manager/)

[![WinGet Package Version][winget-badge]][winget-submission]

The initial `kjanat.actionlint` submission, [microsoft/winget-pkgs#430563][winget-submission], is awaiting review.
The later version submissions remain drafts. Until the package is available in the WinGet source, use [npm](#npm),
[Scoop](#scoop), or a [release archive](#prebuilt-binaries). Once available, install it with:

```powershell
winget install --id kjanat.actionlint --exact --source winget
```

The existing [`rhysd.actionlint` package][winget] installs the upstream project.

### Nixpkgs

The [`actionlint` definition in Nixpkgs][nixpkgs] currently builds the upstream project. This applies to `pkgs.actionlint`,
`nixpkgs#actionlint`, and the older `nix-env` commands. They do not install this fork.

[@voidlily](https://github.com/voidlily) has proposed switching the package to this fork at v1.16.0 in
[NixOS/nixpkgs#561437][nixpkgs-fork-pr]. The PR is open and awaiting review.

Use the [project's flake](#nix) to install this fork directly while the Nixpkgs proposal is pending.

<a id="packages-for-upstream-actionlint"></a>

## Not yet implemented

### [Chocolatey](https://chocolatey.org/)

The [`actionlint` package][chocolatey] installs the upstream project. Packaging this fork is being discussed in
[kai2nenobu/chocolatey-packages#40](https://github.com/kai2nenobu/chocolatey-packages/issues/40).
This fork does not currently publish a Chocolatey package. Use [Scoop](#scoop),
[mise](#mise), or a [release archive](#prebuilt-binaries) on Windows.

### asdf

The [asdf-actionlint plugin][asdf-plugin] downloads upstream releases. This fork does not currently provide an asdf
plugin. Use mise's GitHub backend or a release archive instead.

---

[Checks](checks.md) | [Usage](usage.md) | [Configuration](config.md) | [Go API](api.md) | [References](reference.md)

[formula]: https://formulae.brew.sh/formula/actionlint
[homebrew]: https://brew.sh/
[releases]: https://github.com/kjanat/actionlint/releases
[release-badge]: https://img.shields.io/github/v/release/kjanat/actionlint
[gh]: https://docs.github.com/en/github-cli/github-cli/about-github-cli
[attestations]: https://docs.github.com/en/actions/concepts/security/artifact-attestations
[Go]: https://go.dev/
[go-module]: https://pkg.go.dev/actionlint.kjanat.dev
[go-module-badge]: https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Factionlint.kjanat.dev%2F%40latest&query=%24.Version&label=Go%20module
[asdf-plugin]: https://github.com/crazy-matt/asdf-actionlint
[chocolatey]: https://community.chocolatey.org/packages/actionlint
[docker-badge]: https://img.shields.io/docker/v/kjanat/actionlint
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint
[npm-package]: https://www.npmjs.com/package/@kjanat/actionlint
[npm-badge]: https://img.shields.io/npm/v/%40kjanat%2Factionlint
[pypi-package]: https://pypi.org/project/actionlint-py-kjanat/
[pypi-badge]: https://img.shields.io/pypi/v/actionlint-py-kjanat?label=PyPI%20%28community%29
[python-wrapper]: https://github.com/renefritze/actionlint-py-kjanat
[python-wrapper-pr]: https://github.com/renefritze/actionlint-py-kjanat/pull/1
[uv-tools]: https://docs.astral.sh/uv/guides/tools/
[upstream]: https://github.com/rhysd/actionlint
[scoop]: https://scoop.sh/#/apps?q=actionlint&s=0&d=1&o=true
[scoop-bucket]: https://github.com/kjanat/scoop-bucket/blob/master/bucket/actionlint.json
[scoop-badge]: https://img.shields.io/scoop/v/actionlint?bucket=https%3A%2F%2Fgithub.com%2Fkjanat%2Fscoop-bucket
[winget]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/r/rhysd/actionlint
[winget-submission]: https://github.com/microsoft/winget-pkgs/pull/430563
[winget-badge]: https://img.shields.io/winget/v/kjanat.actionlint
[actionlint-kjanat-git]: https://aur.archlinux.org/packages/actionlint-kjanat-git
[actionlint-kjanat-bin]: https://aur.archlinux.org/packages/actionlint-kjanat-bin
[actionlint-kjanat]: https://aur.archlinux.org/packages/actionlint-kjanat
[aur-git-badge]: https://img.shields.io/aur/version/actionlint-kjanat-git?label=AUR%20%28git%29
[aur-bin-badge]: https://img.shields.io/aur/version/actionlint-kjanat-bin?label=AUR%20%28binary%29
[aur-source-badge]: https://img.shields.io/aur/version/actionlint-kjanat?label=AUR%20%28source%29
[aur]: https://aur.archlinux.org/
[paru]: https://github.com/Morganamilo/paru
[nixpkgs]: https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/ac/actionlint/package.nix
[nixpkgs-fork-pr]: https://github.com/NixOS/nixpkgs/pull/561437
[mise-github]: https://mise.jdx.dev/dev-tools/backends/github.html
