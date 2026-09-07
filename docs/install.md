# Installation

[![GitHub Release][release-badge]][releases]

This document describes how to install the [kjanat/actionlint fork](../README.md). Package names matter: several
registries also provide [rhysd/actionlint][upstream] under the unqualified name `actionlint`.

ShellCheck and pyflakes are optional external linters. The standalone actionlint binary uses them when available on
`PATH`; see [external linter configuration](usage.md#ignore-some-errors).

## Windows

### [Chocolatey](https://chocolatey.org/)

The community [`actionlint` package][chocolatey] installs the upstream project. This fork does not currently publish a
Chocolatey package. Use [Scoop](#scoop), [mise](#mise), or a [release archive](#prebuilt-binaries) on Windows.

### [Scoop](https://scoop.sh/)

[![Scoop Version][scoop-badge]][scoop-bucket]

This fork is available in the [kjanat bucket][scoop-bucket]:

```powershell
scoop bucket add kjanat https://github.com/kjanat/scoop-bucket
scoop install kjanat/actionlint
```

The [`actionlint` package in Scoop's main bucket][scoop] installs the upstream project.

### [Winget](https://learn.microsoft.com/en-us/windows/package-manager/)

[![WinGet Package Version][winget-badge]][winget-submission]

The first submission for this fork, `kjanat.actionlint` (microsoft/winget-pkgs#430563), is awaiting review. Until it is merged and available in the WinGet source, use Scoop or a release archive. Once available, the command will be:

```powershell
winget install --id kjanat.actionlint --exact --source winget
```

The existing [`rhysd.actionlint` package][winget] installs the upstream project.

## Linux

### [Arch Linux](https://archlinux.org/)

[![AUR Version (git)][aur-git-badge]][actionlint-kjanat-git]
[![AUR Version (binary)][aur-bin-badge]][actionlint-kjanat-bin]
[![AUR Version (source)][aur-source-badge]][actionlint-kjanat]

[`actionlint-kjanat-git`][actionlint-kjanat-git] is available in the [AUR][aur] and builds this fork from `master`.
Install it with an AUR helper such as [`paru`][paru]:

```sh
paru -S actionlint-kjanat-git
```

It installs the manpage and shell completions. It conflicts with the upstream `actionlint`, `actionlint-bin`, and
`actionlint-git` packages because they provide the same executable.

The release configuration also defines `actionlint-kjanat` (release source) and `actionlint-kjanat-bin` (prebuilt binary),
but those two packages have not yet been published to the AUR. For a stable fork release, use a release archive or mise.

### [Nix](https://nix.dev/)

The [`actionlint` definition in Nixpkgs][nixpkgs] builds the upstream project. This applies to `pkgs.actionlint`,
`nixpkgs#actionlint`, and the older `nix-env` commands. They do not install this fork.

This repository does not currently provide a Nix derivation or flake. The fork's statically linked Linux release
binary runs on NixOS; use a [release archive](#prebuilt-binaries) or the [download script](#download-script) to obtain it.
That installs a standalone executable rather than a Nix-managed package.

<a id="homebrew"></a>

## [Homebrew][homebrew] on macOS and Linux

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

## [npm](https://www.npmjs.com/)

[![NPM Version][npm-badge]][npm-package]

Publishing for [`@kjanat/actionlint`][npm-package] is implemented, but its first package has not yet been published to
the public npm registry. Until then, use a release archive or mise. Once published, it will install a prebuilt binary:

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it without adding it to the project:

```sh
npx @kjanat/actionlint
```

The planned distribution uses platform packages containing the GitHub release binaries. Keep npm's optional
dependencies enabled, since the launcher needs the package matching your operating system and architecture.
Linux binaries are statically linked and work with both musl and glibc.

## Prebuilt binaries

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
gh release download --repo kjanat/actionlint --pattern '*_linux_amd64.tar.gz' v1.15.0
tar xf actionlint_1.15.0_linux_amd64.tar.gz
./actionlint -version
```

Verify the downloaded archive's [build provenance attestation][attestations] against this repository:

```sh
gh attestation verify -R kjanat/actionlint actionlint_1.15.0_linux_amd64.tar.gz
```

<a id="download-script"></a>

## Download script

To install `actionlint` executable with one command, [the download script](../scripts/download-actionlint.bash) is available.
It downloads `actionlint.exe` on Windows and `actionlint` on other supported platforms. Pass `latest` to resolve the
newest release, or omit the argument to use the default version recorded in the script:

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) latest
```

When you need to install specific version of actionlint, please give the version to the 1st command line argument. The following
example installs v1.15.0.

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) 1.15.0
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

## Docker image

[![Docker Image Version][docker-badge]][dockerhub]

The fork publishes CLI images as `ghcr.io/kjanat/actionlint` and `docker.io/kjanat/actionlint`. See
[Docker usage](usage.md#docker) for running the linter with a mounted repository.

## Cross-platform version managers

### asdf

The [asdf-actionlint plugin][asdf-plugin] downloads upstream releases. This fork does not currently provide an asdf
plugin. Use mise's GitHub backend or a release archive instead.

### mise

Use [mise's GitHub backend][mise-github] with this repository's full name. The short `actionlint` tool name resolves to
upstream packages in mise's registry.

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

## Build from source

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
[mise-github]: https://mise.jdx.dev/dev-tools/backends/github.html
