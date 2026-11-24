# Installation

[![GitHub Release][release-badge]][releases]

Several registries publish [rhysd/actionlint][upstream] under the plain name `actionlint`. Use the package names below to install this fork.

Action discovery inside a Git repository needs `git` 2.8 or newer. The Docker image and the Nix, AUR and Scoop packages include it. Other installs need `git` on `PATH`.

ShellCheck and Ruff run as Wasm plugins. Run `actionlint config init` in a new project, or follow [configuration migration](config-migration.md) in an existing one. Commit the configuration with its pinned plugin versions. See [plugin installation](plugins.md#install-from-the-registry).

## Project-maintained distributions

### Windows

#### [Scoop](https://scoop.sh/ "scoop.sh homepage")

[![Scoop Version][scoop-badge]][scoop-bucket]

```powershell
scoop bucket add kjanat https://github.com/kjanat/scoop-bucket
scoop install kjanat/actionlint
```

#### [WinGet](https://learn.microsoft.com/en-us/windows/package-manager/ "winget homepage")

[![WinGet Package Version][winget-badge]][winget-package]

```powershell
winget install --id kjanat.actionlint --exact --source winget
```

### Linux

#### [Arch Linux](https://archlinux.org/ "arch linux homepage")

[![AUR Version (git)][aur-git-badge]][actionlint-kjanat-git]
[![AUR Version (binary)][aur-bin-badge]][actionlint-kjanat-bin]
[![AUR Version (source)][aur-source-badge]][actionlint-kjanat]

| Package                                          | Installs                                      |
| ------------------------------------------------ | --------------------------------------------- |
| [`actionlint-kjanat-bin`][actionlint-kjanat-bin] | The prebuilt binary from a stable release     |
| [`actionlint-kjanat`][actionlint-kjanat]         | A stable release built from source            |
| [`actionlint-kjanat-git`][actionlint-kjanat-git] | The current `master` branch built from source |

The three packages conflict with each other and with the upstream `actionlint`, `actionlint-bin` and `actionlint-git` packages.

<a id="homebrew"></a>

### [Homebrew][homebrew] on macOS and Linux

```sh
brew install --cask kjanat/tap/actionlint
```

For native ShellCheck, install the `kjanat/tap/shellcheck` cask. Uninstall the homebrew-core `shellcheck` formula first if you have it:

```sh
brew install --cask kjanat/tap/shellcheck
```

For a native [Ruff backend](config.md#ruff), install Ruff:

```sh
brew install ruff
```

> [!WARNING]
> Apple has not notarized the macOS executable. If Gatekeeper blocks it, allow it in **System Settings → Privacy & Security**.

### [npm](https://www.npmjs.com/ "npmjs.com homepage")

[![NPM Version][npm-badge]][npm-package]

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it without installing:

```sh
npx @kjanat/actionlint
```

Keep npm's optional dependencies enabled. The launcher needs the package for your platform.

### [PyPI](https://pypi.org/ "pypi.org homepage")

[![PyPI Version][pypi-kjanat-badge]][pypi-kjanat]

```sh
python -m pip install actionlint-kjanat
```

Or run it with [uv][uv-tools]:

```sh
uvx --from actionlint-kjanat actionlint
```

### Prebuilt binaries

Download an archive from [the releases page][releases], extract it, and put the executable on `$PATH`. Archives exist for:

- macOS (x86_64, arm64)
- Linux (i386, x86_64, ARMv6-compatible 32-bit ARM, arm64)
- Windows (i386, x86_64, arm64)
- FreeBSD (i386, x86_64)

With [`gh`][gh]:

```sh
gh release download --repo kjanat/actionlint --pattern '*_linux_amd64.tar.gz' v2.0.0
tar xf actionlint_2.0.0_linux_amd64.tar.gz
./actionlint --version
```

Verify the archive's [build provenance attestation][attestations]:

```sh
gh attestation verify -R kjanat/actionlint actionlint_2.0.0_linux_amd64.tar.gz
```

<a id="download-script"></a>

### Download script

[The download script](../distribution/install/download-actionlint.bash) installs the executable in one command. Pass `latest` for the newest release:

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/d517adb1a17d8448608ff00dfd968a15180ef569/distribution/install/download-actionlint.bash) latest
```

Pass a version to install that release:

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/d517adb1a17d8448608ff00dfd968a15180ef569/distribution/install/download-actionlint.bash) 2.0.0
```

The script writes to the current directory. Pass an existing directory as the second argument to write there:

```sh
mkdir -p "${HOME}/.local/bin"
bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/d517adb1a17d8448608ff00dfd968a15180ef569/distribution/install/download-actionlint.bash) latest "${HOME}/.local/bin"
```

With an authenticated `gh`, the script verifies the archive's attestation. It extracts only the executable.

### Docker image

[![Docker Image Version][docker-badge]][dockerhub]

The images are `ghcr.io/kjanat/actionlint` and `docker.io/kjanat/actionlint`. See [Docker usage](usage.md#docker).

Both registries also carry these Linux amd64 and arm64 tags:

- `:branch-<name>` for branch pushes, such as `:branch-master`. `feature/cache` becomes `branch-feature-cache`.
- `:nightly`, `:nightly-<git-describe>` and `:sha-<full-commit-SHA>` after successful master CI.

A rebuild can move these tags. Pin a digest for identical bytes.

### Cross-platform version managers

#### aqua

[![aqua registry: kjanat/actionlint][aqua-badge]][aqua-package]

The [aqua registry entry][aqua-package] verifies checksums and attestations:

```sh
aqua g -i kjanat/actionlint
```

#### mise

| Backend                                                   | Project-local command                                 | Installation                                                      |
| --------------------------------------------------------- | ----------------------------------------------------- | ----------------------------------------------------------------- |
| [GitHub][mise-github]                                     | `mise use github:kjanat/actionlint`                   | Prebuilt release binary.                                          |
| [aqua](https://mise.jdx.dev/dev-tools/backends/aqua.html) | `mise use aqua:kjanat/actionlint`                     | Prebuilt binary with aqua's checks. Needs mise 2026.9.7 or newer. |
| [npm](https://mise.jdx.dev/dev-tools/backends/npm.html)   | `mise use 'npm:@kjanat/actionlint'`                   | npm launcher. Needs Node.js and npm optional dependencies.        |
| [Go](https://mise.jdx.dev/dev-tools/backends/go.html)     | `mise use go:actionlint.kjanat.dev/v2/cmd/actionlint` | Builds from source with `go install`.                             |

### [Nix](https://nix.dev/ "nix.dev homepage")

The project's flake builds this fork from source. With the `nix-command` and `flakes` features enabled, run it without installing:

```sh
nix run github:kjanat/actionlint -- --help
```

Or install it into your profile:

```sh
nix profile add github:kjanat/actionlint
```

The package includes the manpage, shell completions and the configuration schema. These commands build the default branch. Put a release tag or commit in the flake reference to pin a version. The flake supports x86-64 and ARM64 Linux and Apple silicon macOS.

### Build from source

[![Go Module Version][go-module-badge]][go-module]

Use a [Go][Go] toolchain that the revision's [`go.mod`](../go.mod) accepts.

```sh
# Install the latest stable version
go install actionlint.kjanat.dev/v2/cmd/actionlint@latest

# Install the head of the master branch
go install actionlint.kjanat.dev/v2/cmd/actionlint@master
```

### pre-commit

Use the hooks from [kjanat/actionlint-pre-commit](https://github.com/kjanat/actionlint-pre-commit). The first v2 hook release is pending. The hook installs the matching wheel. See [v2 migration](migration-v2.md#pre-commit-repository) and the [usage guide](usage.md#pre-commit).

### GitHub Action

The [`kjanat/actionlint` Action](action.md) installs and runs actionlint. The v2 Action is not published yet.

```yaml
- uses: actions/checkout@v7
  with: { persist-credentials: false }
- uses: kjanat/actionlint@v2
```

To install actionlint for later steps without running it:

```yaml
- uses: kjanat/actionlint@v2
  with:
    install-only: true
- run: actionlint --version
```

`install-only` installs no plugins and no native `shellcheck` or `ruff`. See the [Action guide](action.md#install-without-running-analysis).

## Community-maintained integrations

### Python (pip and uv)

[![PyPI Version][pypi-badge]][pypi-package]

[`actionlint-py-kjanat`][pypi-package] is a community wrapper by [René Fritze (@renefritze)][python-wrapper].

```sh
python -m pip install actionlint-py-kjanat
actionlint --version
```

Or run it with [uv][uv-tools]:

```sh
uvx --from actionlint-py-kjanat actionlint
```

Installation downloads the binary from this repository's GitHub releases and checks its SHA-256 checksum. The wrapper may package an older actionlint release.

<a id="packages-for-upstream-actionlint"></a>

## Packages for upstream actionlint

These packages install upstream actionlint:

| Package                  |
| ------------------------ |
| [Nixpkgs][nixpkgs]       |
| [Chocolatey][chocolatey] |
| [asdf][asdf-plugin]      |
| [Trunk][trunk-plugin]    |

Use the [project's flake](#nix), [mise](#mise) or a [release archive](#prebuilt-binaries) instead.

---

[Checks](checks.md) | [Usage](usage.md) | [Configuration](config.md) | [Integration interfaces](api.md) | [References](reference.md)

[Go]: https://go.dev/
[actionlint-kjanat-bin]: https://aur.archlinux.org/packages/actionlint-kjanat-bin "AUR package for kjanat/actionlint (binary)"
[actionlint-kjanat-git]: https://aur.archlinux.org/packages/actionlint-kjanat-git "AUR package for kjanat/actionlint (git)"
[actionlint-kjanat]: https://aur.archlinux.org/packages/actionlint-kjanat "AUR package for kjanat/actionlint"
[aqua-badge]: https://img.shields.io/badge/dynamic/yaml?url=https%3A%2F%2Fraw.githubusercontent.com%2Faquaproj%2Faqua-registry%2Fmain%2Fpkgs%2Fkjanat%2Factionlint%2Fregistry.yaml&query=%24.packages%5B0%5D.repo_name&prefix=kjanat%2F&label=aqua%20registry&color=blue
[aqua-package]: https://github.com/aquaproj/aqua-registry/tree/main/pkgs/kjanat/actionlint "aqua registry entry for kjanat/actionlint"
[asdf-plugin]: https://github.com/crazy-matt/asdf-actionlint "asdf-actionlint plugin for asdf version manager"
[attestations]: https://docs.github.com/en/actions/concepts/security/artifact-attestations "GitHub artifact attestations documentation"
[aur-bin-badge]: https://img.shields.io/aur/version/actionlint-kjanat-bin?logo=archlinux&logoColor=white&label=AUR%20%28binary%29&labelColor=1793D1&color=black
[aur-git-badge]: https://img.shields.io/aur/version/actionlint-kjanat-git?logo=archlinux&logoColor=white&label=AUR%20%28git%29&labelColor=1793D1&color=black
[aur-source-badge]: https://img.shields.io/aur/version/actionlint-kjanat?logo=archlinux&logoColor=white&label=AUR%20%28source%29&labelColor=1793D1&color=black
[chocolatey]: https://community.chocolatey.org/packages/actionlint "Chocolatey package"
[docker-badge]: https://img.shields.io/docker/v/kjanat/actionlint?logo=docker&logoColor=white&label=Docker&labelColor=2496ED&color=black
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint "Docker Hub"
[gh]: https://docs.github.com/en/github-cli/github-cli/about-github-cli "gh"
[go-module-badge]: https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Factionlint.kjanat.dev%2Fv2%2F%40latest&query=%24.Version&logo=go&logoColor=white&label=Go%20module&labelColor=00ADD8&color=black
[go-module]: https://pkg.go.dev/actionlint.kjanat.dev/v2 "Go module documentation"
[homebrew]: https://brew.sh/ "homebrew homepage"
[mise-github]: https://mise.jdx.dev/dev-tools/backends/github.html "mise GitHub backend"
[nixpkgs]: https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/ac/actionlint/package.nix "nixpkgs package definition for actionlint"
[npm-badge]: https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black
[npm-package]: https://npm.im/@kjanat/actionlint "npm package for kjanat/actionlint"
[pypi-badge]: https://img.shields.io/pypi/v/actionlint-py-kjanat?logo=data%3Aimage%2Fsvg%2Bxml%3Bbase64%2CPHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAzMS42OTYgMzAuMDI0Ij48ZyBzdHJva2U9IiNjY2MiIHN0cm9rZS1saW5lam9pbj0iYmV2ZWwiIHN0cm9rZS13aWR0aD0iLjM1NSI%2BPHBhdGggZmlsbD0iI2Y3ZjdmNCIgZD0ibS4xNzggNS45MTIgMTUuNTU1IDUuNjYyTDMxLjUxOSA1LjgzIDE1Ljk2My4xNjd6Ii8%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTE1LjczMyAxMS41NzR2MTguMjgzbDE1Ljc4Ni01Ljc0NlY1LjgzeiIvPjxwYXRoIGZpbGw9IiNlZmVlZWEiIGQ9Im0uMTc4IDUuOTEyIDE1LjU1NSA1LjY2MnYxOC4yODNMLjE3OCAyNC4xOTV6Ii8%2BPC9nPjwvc3ZnPg%3D%3D&color=3775A9
[pypi-kjanat-badge]: https://img.shields.io/pypi/v/actionlint-kjanat?logo=pypi&logoColor=white&label=PyPI&labelColor=3775A9&color=black
[pypi-kjanat]: https://pypi.org/project/actionlint-kjanat/ "PyPI package for kjanat/actionlint"
[pypi-package]: https://pypi.org/project/actionlint-py-kjanat/ "PyPI package for kjanat/actionlint Python wrapper"
[python-wrapper]: https://github.com/renefritze/actionlint-py-kjanat "René Fritze's GitHub repository for the Python wrapper"
[release-badge]: https://img.shields.io/github/v/release/kjanat/actionlint?logo=github&logoColor=white&label=release&labelColor=181717&color=black
[releases]: https://github.com/kjanat/actionlint/releases "GitHub releases for kjanat/actionlint"
[scoop-badge]: https://img.shields.io/scoop/v/actionlint?bucket=https%3A%2F%2Fgithub.com%2Fkjanat%2Fscoop-bucket&label=Scoop&labelColor=E83F5B&color=black
[scoop-bucket]: https://github.com/kjanat/scoop-bucket/blob/master/bucket/actionlint.json "Scoop bucket for kjanat/actionlint"
[trunk-plugin]: https://github.com/trunk-io/plugins/tree/main/linters/actionlint "Trunk actionlint plugin"
[upstream]: https://github.com/rhysd/actionlint "Upstream actionlint project"
[uv-tools]: https://docs.astral.sh/uv/guides/tools/ "uv tools documentation"
[winget-badge]: https://img.shields.io/winget/v/kjanat.actionlint?label=WinGet&labelColor=0078D4&color=black&logo=data%3Aimage%2Fsvg%2Bxml%3Bbase64%2CPHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMTIuNSAxODAgMTUwIj4KICA8cmVjdCB4PSI0MS44MTciIHk9IjIxLjM5NCIgd2lkdGg9Ijk2IiBoZWlnaHQ9Ijk2IiByeD0iOCIgZmlsbD0iIzljNjQwYSIvPgogIDxyZWN0IHg9IjMzLjgxNyIgeT0iMzcuNSIgd2lkdGg9IjExMiIgaGVpZ2h0PSI5NiIgcng9IjgiIGZpbGw9IiNiYzgyMmEiLz4KICA8cmVjdCB4PSIyNS44MTciIHk9IjUzLjQxOSIgd2lkdGg9IjEyOCIgaGVpZ2h0PSI5NiIgcng9IjgiIGZpbGw9IiNkY2IzNzQiLz4KICA8cGF0aCBkPSJNOTAgNzd2NDguNU03MCAxMDUuNWwyMCAyMCAyMC0yMCIgZmlsbD0ibm9uZSIgc3Ryb2tlPSIjZmZmIiBzdHJva2Utd2lkdGg9IjE2IiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiLz4KPC9zdmc%2BCg%3D%3D&logoSize=auto
[winget-package]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/k/kjanat/actionlint "WinGet package for kjanat/actionlint"
