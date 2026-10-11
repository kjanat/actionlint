# Installation

[![GitHub Release][release-badge]][releases]

This document describes how to install the [kjanat/actionlint fork](../README.md). Package names matter: several
registries also provide [rhysd/actionlint][upstream] under the unqualified name `actionlint`.

ShellCheck and Ruff are optional external linters. Standalone archives, Go installs, and npm packages contain
actionlint only; install `shellcheck` and `ruff` separately on `PATH` to check shell and Python scripts.
See [external linter configuration](usage.md#ignore-some-errors).

Images built from this revision bundle both native executables, without requiring Python. The Nix package
also provides both through its wrapper. The [`actionlint-bundled` pre-commit hook](usage.md#pre-commit)
builds the Docker image from its selected source revision and includes both tools.
Previously published releases are unchanged and do not include Ruff.

## Project-maintained distributions

These methods install packages, binaries, or source published by this project.

[Scoop](#scoop) · [WinGet](#winget) · [AUR](#arch-linux) · [Homebrew](#homebrew) · [npm](#npm) · [Archives](#prebuilt-binaries) ·
[Download script](#download-script) · [Docker](#docker-image) · [aqua](#aqua) · [mise](#mise) · [Nix](#nix) · [Go](#build-from-source)
· [pre-commit](#pre-commit) · [GitHub Action](#github-action)

[Community-maintained](#community-maintained-integrations): [Python (pip and uv)](#python-pip-and-uv).

[Pending](#pending-packages): [Nixpkgs](#nixpkgs).
[Not yet available for this fork](#not-yet-implemented): [Chocolatey](#chocolatey) · [asdf](#asdf) · [Trunk](#trunk).

### Windows

#### [Scoop](https://scoop.sh/ "scoop.sh homepage")

[![Scoop Version][scoop-badge]][scoop-bucket]

This fork is available in the [kjanat bucket][scoop-bucket]:

```powershell
scoop bucket add kjanat https://github.com/kjanat/scoop-bucket
scoop install kjanat/actionlint
```

The [`actionlint` package in Scoop's main bucket][scoop] installs the upstream project.

#### [WinGet](https://learn.microsoft.com/en-us/windows/package-manager/ "winget homepage")

[![WinGet Package Version][winget-badge]][winget-package]

Install this fork with:

```powershell
winget install --id kjanat.actionlint --exact --source winget
```

The existing [`rhysd.actionlint` package][winget] installs the upstream project.

### Linux

#### [Arch Linux](https://archlinux.org/ "arch linux homepage")

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

The cask has no package dependencies. ShellCheck and Ruff are optional and must be installed separately on `PATH`.
For [ShellCheck integration](checks.md#check-shellcheck-integ),
the `kjanat/tap/shellcheck` cask provides the upstream static binary and shell completions with no dependencies.
Uninstall the homebrew-core formula first if it already owns the `shellcheck` binary:

```sh
brew install --cask kjanat/tap/shellcheck
```

For [Ruff integration](config.md#ruff), install [Ruff's Homebrew package](https://docs.astral.sh/ruff/installation/):

```sh
brew install ruff
```

> [!WARNING]
> The macOS executable is not notarized. If Gatekeeper blocks a downloaded copy, review and allow it in
> **System Settings → Privacy & Security**.

### [npm](https://www.npmjs.com/ "npmjs.com homepage")

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
gh release download --repo kjanat/actionlint --pattern '*_linux_amd64.tar.gz' v1.17.0
tar xf actionlint_1.17.0_linux_amd64.tar.gz
./actionlint -version
```

Verify the downloaded archive's [build provenance attestation][attestations] against this repository:

```sh
gh attestation verify -R kjanat/actionlint actionlint_1.17.0_linux_amd64.tar.gz
```

<a id="download-script"></a>

### Download script

To install `actionlint` executable with one command, [the download script](../scripts/download-actionlint.bash) is available.

The examples pin the script to a commit. The separate version argument selects the binary release; `latest` resolves
the newest release. Update the script pin when adopting script changes, independently of the binary version.
It downloads `actionlint.exe` on Windows and `actionlint` on other supported platforms. Pass `latest` to resolve the
newest release, or omit the argument to use the default version recorded in the script:

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) latest
```

When you need to install specific version of actionlint, please give the version to the 1st command line argument. The following
example installs v1.17.0.

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 1.17.0
```

This script downloads `actionlint` (or `actionlint.exe` on Windows) binary to the current working directory. When you need to put
the downloaded binary to some other directory, please give the directory path to the 2nd command line argument. The following
example installs the latest version to `~/.local/bin`. The destination must already exist and be on your `PATH`:

```sh
mkdir -p "${HOME}/.local/bin"
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) latest "${HOME}/.local/bin"
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

#### aqua

[![aqua registry: kjanat/actionlint][aqua-badge]][aqua-package]

The [aqua registry entry][aqua-package]
installs this fork's release binaries and specifies SHA-256 checksums and GitHub artifact attestations for verification.
With [aqua](https://aquaproj.github.io/) installed and an initialized `aqua.yaml`, use a standard registry version
containing aquaproj/aqua-registry#60566, then add and install the package:

```sh
aqua g -i kjanat/actionlint
```

Use the full `kjanat/actionlint` name to select this fork; the upstream package is `rhysd/actionlint`.

#### mise

Choose one backend for the `actionlint` executable:

| Backend                                                   | Project-local command                              | Installation                                                                                             |
| --------------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| [GitHub][mise-github]                                     | `mise use github:kjanat/actionlint`                | Prebuilt release binary; independent of registry snapshots.                                              |
| [aqua](https://mise.jdx.dev/dev-tools/backends/aqua.html) | `mise use aqua:kjanat/actionlint`                  | Prebuilt release binary using aqua's checksum and attestation metadata; see registry availability below. |
| [npm](https://mise.jdx.dev/dev-tools/backends/npm.html)   | `mise use 'npm:@kjanat/actionlint'`                | The npm launcher and its platform binary; requires Node.js at runtime and optional dependencies enabled. |
| [Go](https://mise.jdx.dev/dev-tools/backends/go.html)     | `mise use go:actionlint.kjanat.dev/cmd/actionlint` | Builds from source with `go install`; requires a compatible Go toolchain.                                |

The short `actionlint` tool name resolves to upstream packages in mise's registry. Use the full identifiers above
to select this fork. The npm route installs the executable into mise's tool directory; use
[`npm install --save-dev`](#npm) instead when it belongs in your project's dependencies.
For the Go route, see [source-build requirements](#build-from-source).

For example, using the GitHub backend:

```bash
# Show all installable versions
mise ls-remote github:kjanat/actionlint

# Install the latest release
mise install github:kjanat/actionlint@latest

# Set a version globally (on your ~/.config/mise/config.toml file)
mise use -g github:kjanat/actionlint@latest
```

For a project-local selection, put this in `mise.toml` and run `mise install`:

```toml
[tools]
"github:kjanat/actionlint" = "latest"
```

Alternatively, [mise's built-in aqua backend](https://mise.jdx.dev/dev-tools/backends/aqua.html) uses the same
registry metadata for checksum and attestation verification, without installing the aqua CLI:

```sh
mise use aqua:kjanat/actionlint
```

mise bundles its registry snapshot. The entry is included in the pending
[mise 2026.9.7 release PR](https://github.com/jdx/mise/pull/13125); use a release containing that update.
On mise 2026.9.6, opt into the current registry through `mise.toml` to use the entry before that release:

```toml
[settings]
registry_floating = true

[tools]
"aqua:kjanat/actionlint" = "latest"
```

This [setting](https://mise.jdx.dev/configuration/settings.html#registry_floating "registry_floating") floats both mise and aqua registries.
The GitHub backend examples above work independently of the bundled registry.

### [Nix](https://nix.dev/ "nix.dev homepage")

The project's flake builds this fork from source. With Nix's `nix-command` and `flakes` features enabled, run it
without installing it into your profile:

```sh
nix run github:kjanat/actionlint -- --help
```

Or install it into your profile:

```sh
nix profile add github:kjanat/actionlint
```

The package includes ShellCheck and Ruff, the manpage, Bash/Zsh/Fish completions, and the configuration schema
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

### pre-commit

Install [pre-commit](https://pre-commit.com/#installation), then add this to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.17.0
    hooks:
      - id: actionlint
```

Install the Git hook and check all workflow files:

```sh
pre-commit install
pre-commit run actionlint --all-files
```

This project's [hook manifest](../.pre-commit-hooks.yaml) provides five installation options:

- `actionlint`: builds actionlint with Go. ShellCheck and Ruff are separate installations on `PATH`.
- `actionlint-shellcheck`: builds actionlint and ShellCheck with Go. Ruff is a separate installation.
- `actionlint-docker`: pulls the project's published container image; requires Docker.
- `actionlint-bundled`: builds the selected revision's container image with ShellCheck and Ruff; requires Docker.
- `actionlint-system`: runs your existing `actionlint` installation on `PATH`.

Choose a release or commit containing the hook you want. `actionlint-bundled` is new and is not available in
v1.17.0. See [the pre-commit guide](usage.md#pre-commit) for hook selection and
[updating pinned revisions](usage.md#updating-pre-commit-hooks).

### GitHub Action

The project's [`kjanat/actionlint` Action](action.md) installs actionlint and runs it in GitHub Actions:

```yaml
- uses: actions/checkout@v7
  with: { persist-credentials: false }
- uses: kjanat/actionlint@v1
```

The upcoming Node-based Action also supports installation for later steps in the same job:

```yaml
- uses: kjanat/actionlint@v1
  with:
    install-only: true
    shellcheck: false
    ruff: false
- run: actionlint --version
```

Install-only requires an Action release containing that feature. Existing immutable releases retain their
original implementation. Pin a published release commit SHA or a specific release tag for reproducible setup.
Enable the `shellcheck` and `ruff` inputs to install those tools too. See the
[Action installation guide](action.md#install-without-running-analysis) for version selection and prerequisites.

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

### Nixpkgs

The [`actionlint` definition in Nixpkgs][nixpkgs] currently builds the upstream project. This applies to `pkgs.actionlint`,
`nixpkgs#actionlint`, and the older `nix-env` commands. They do not install this fork.

[@voidlily](https://github.com/voidlily) has proposed switching the package to this fork at v1.16.0 in
NixOS/nixpkgs#561437. The PR is open and awaiting review.

Use the [project's flake](#nix) to install this fork directly while the Nixpkgs proposal is pending.

<a id="packages-for-upstream-actionlint"></a>

## Not yet implemented

### [Chocolatey](https://chocolatey.org/ "chocolatey.org homepage")

The [`actionlint` package][chocolatey] installs the upstream project. Packaging this fork is being discussed in
kai2nenobu/chocolatey-packages#40.
This fork does not currently publish a Chocolatey package. Use [Scoop](#scoop),
[mise](#mise), or a [release archive](#prebuilt-binaries) on Windows.

### asdf

The [asdf-actionlint plugin][asdf-plugin] currently downloads upstream releases.
[crazy-matt/asdf-actionlint#18][asdf-proposal] proposes switching it to `kjanat/actionlint`; the PR is open.
Until it is merged, use [mise's GitHub backend](#mise) or a [release archive](#prebuilt-binaries) to install this fork.

### Trunk

The [Trunk actionlint plugin][trunk-plugin] currently installs upstream releases.
[trunk-io/plugins#1181][trunk-proposal] proposes switching it to `kjanat/actionlint`; the PR is open.
Until it is merged, use one of the [project-maintained distributions](#project-maintained-distributions) to install this fork.

---

[Checks](checks.md) | [Usage](usage.md) | [Configuration](config.md) | [Go API](api.md) | [References](reference.md)

[Go]: https://go.dev/
[actionlint-kjanat-bin]: https://aur.archlinux.org/packages/actionlint-kjanat-bin "AUR package for kjanat/actionlint (binary)"
[actionlint-kjanat-git]: https://aur.archlinux.org/packages/actionlint-kjanat-git "AUR package for kjanat/actionlint (git)"
[actionlint-kjanat]: https://aur.archlinux.org/packages/actionlint-kjanat "AUR package for kjanat/actionlint"
[aqua-badge]: https://img.shields.io/badge/dynamic/yaml?url=https%3A%2F%2Fraw.githubusercontent.com%2Faquaproj%2Faqua-registry%2Fmain%2Fpkgs%2Fkjanat%2Factionlint%2Fregistry.yaml&query=%24.packages%5B0%5D.repo_name&prefix=kjanat%2F&label=aqua%20registry&color=blue
[aqua-package]: https://github.com/aquaproj/aqua-registry/tree/main/pkgs/kjanat/actionlint "aqua registry entry for kjanat/actionlint"
[asdf-plugin]: https://github.com/crazy-matt/asdf-actionlint "asdf-actionlint plugin for asdf version manager"
[asdf-proposal]: https://github.com/crazy-matt/asdf-actionlint/pull/18 "asdf-actionlint PR proposing to switch to kjanat/actionlint"
[attestations]: https://docs.github.com/en/actions/concepts/security/artifact-attestations "GitHub artifact attestations documentation"
[aur-bin-badge]: https://img.shields.io/aur/version/actionlint-kjanat-bin?logo=archlinux&logoColor=white&label=AUR%20%28binary%29&labelColor=1793D1&color=black
[aur-git-badge]: https://img.shields.io/aur/version/actionlint-kjanat-git?logo=archlinux&logoColor=white&label=AUR%20%28git%29&labelColor=1793D1&color=black
[aur-source-badge]: https://img.shields.io/aur/version/actionlint-kjanat?logo=archlinux&logoColor=white&label=AUR%20%28source%29&labelColor=1793D1&color=black
[aur]: https://aur.archlinux.org/ "Arch User Repository homepage"
[chocolatey]: https://community.chocolatey.org/packages/actionlint "Chocolatey package"
[docker-badge]: https://img.shields.io/docker/v/kjanat/actionlint?logo=docker&logoColor=white&label=Docker&labelColor=2496ED&color=black
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint "Docker Hub"
[formula]: https://formulae.brew.sh/formula/actionlint "brew formula"
[gh]: https://docs.github.com/en/github-cli/github-cli/about-github-cli "gh"
[go-module-badge]: https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Factionlint.kjanat.dev%2F%40latest&query=%24.Version&logo=go&logoColor=white&label=Go%20module&labelColor=00ADD8&color=black
[go-module]: https://pkg.go.dev/actionlint.kjanat.dev "Go module documentation"
[homebrew]: https://brew.sh/ "homebrew homepage"
[mise-github]: https://mise.jdx.dev/dev-tools/backends/github.html "mise GitHub backend"
[nixpkgs]: https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/ac/actionlint/package.nix "nixpkgs package definition for actionlint"
[npm-badge]: https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black
[npm-package]: https://npm.im/@kjanat/actionlint "npm package for kjanat/actionlint"
[paru]: https://github.com/Morganamilo/paru "paru AUR helper"
[pypi-badge]: https://img.shields.io/pypi/v/actionlint-py-kjanat?logo=data%3Aimage%2Fsvg%2Bxml%3Bbase64%2CPHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAzMS42OTYgMzAuMDI0Ij48ZyBzdHJva2U9IiNjY2MiIHN0cm9rZS1saW5lam9pbj0iYmV2ZWwiIHN0cm9rZS13aWR0aD0iLjM1NSI%2BPHBhdGggZmlsbD0iI2Y3ZjdmNCIgZD0ibS4xNzggNS45MTIgMTUuNTU1IDUuNjYyTDMxLjUxOSA1LjgzIDE1Ljk2My4xNjd6Ii8%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTE1LjczMyAxMS41NzR2MTguMjgzbDE1Ljc4Ni01Ljc0NlY1LjgzeiIvPjxwYXRoIGZpbGw9IiNlZmVlZWEiIGQ9Im0uMTc4IDUuOTEyIDE1LjU1NSA1LjY2MnYxOC4yODNMLjE3OCAyNC4xOTV6Ii8%2BPC9nPjwvc3ZnPg%3D%3D&color=3775A9
[pypi-package]: https://pypi.org/project/actionlint-py-kjanat/ "PyPI package for kjanat/actionlint Python wrapper"
[python-wrapper-pr]: https://github.com/renefritze/actionlint-py-kjanat/pull/1 "PR switching the Python wrapper to kjanat/actionlint"
[python-wrapper]: https://github.com/renefritze/actionlint-py-kjanat "René Fritze's GitHub repository for the Python wrapper"
[release-badge]: https://img.shields.io/github/v/release/kjanat/actionlint?logo=github&logoColor=white&label=release&labelColor=181717&color=black
[releases]: https://github.com/kjanat/actionlint/releases "GitHub releases for kjanat/actionlint"
[scoop-badge]: https://img.shields.io/scoop/v/actionlint?bucket=https%3A%2F%2Fgithub.com%2Fkjanat%2Fscoop-bucket&label=Scoop&labelColor=E83F5B&color=black
[scoop-bucket]: https://github.com/kjanat/scoop-bucket/blob/master/bucket/actionlint.json "Scoop bucket for kjanat/actionlint"
[scoop]: https://scoop.sh/#/apps?q=actionlint&s=0&d=1&o=true "Search Scoop for actionlint"
[trunk-plugin]: https://github.com/trunk-io/plugins/tree/main/linters/actionlint "Trunk actionlint plugin"
[trunk-proposal]: https://github.com/trunk-io/plugins/pull/1181 "Trunk actionlint plugin PR proposing to switch to kjanat/actionlint"
[upstream]: https://github.com/rhysd/actionlint "Upstream actionlint project"
[uv-tools]: https://docs.astral.sh/uv/guides/tools/ "uv tools documentation"
[winget-badge]: https://img.shields.io/winget/v/kjanat.actionlint?label=WinGet&labelColor=0078D4&color=black&logo=data%3Aimage%2Fsvg%2Bxml%3Bbase64%2CPHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMTIuNSAxODAgMTUwIj4KICA8cmVjdCB4PSI0MS44MTciIHk9IjIxLjM5NCIgd2lkdGg9Ijk2IiBoZWlnaHQ9Ijk2IiByeD0iOCIgZmlsbD0iIzljNjQwYSIvPgogIDxyZWN0IHg9IjMzLjgxNyIgeT0iMzcuNSIgd2lkdGg9IjExMiIgaGVpZ2h0PSI5NiIgcng9IjgiIGZpbGw9IiNiYzgyMmEiLz4KICA8cmVjdCB4PSIyNS44MTciIHk9IjUzLjQxOSIgd2lkdGg9IjEyOCIgaGVpZ2h0PSI5NiIgcng9IjgiIGZpbGw9IiNkY2IzNzQiLz4KICA8cGF0aCBkPSJNOTAgNzd2NDguNU03MCAxMDUuNWwyMCAyMCAyMC0yMCIgZmlsbD0ibm9uZSIgc3Ryb2tlPSIjZmZmIiBzdHJva2Utd2lkdGg9IjE2IiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiLz4KPC9zdmc%2BCg%3D%3D&logoSize=auto
[winget-package]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/k/kjanat/actionlint "WinGet package for kjanat/actionlint"
[winget]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/r/rhysd/actionlint "WinGet package for rhysd/actionlint"
