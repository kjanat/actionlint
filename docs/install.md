# Installation

This document describes how to install [actionlint](../docs).

## Windows

### [Chocolatey](https://chocolatey.org/)

[`actionlint` package][chocolatey] is available in the community repository:

```powershell
choco install actionlint
```

### [Scoop](https://scoop.sh/)

[`actionlint` package][scoop] is available in the main bucket:

```powershell
scoop install actionlint
```

That package is [the upstream project][upstream]. This fork lives in its own bucket, which also carries other `kjanat`
tools:

```powershell
scoop bucket add kjanat https://github.com/kjanat/scoop-bucket
scoop install kjanat/actionlint
```

### [Winget](https://learn.microsoft.com/en-us/windows/package-manager/)

[`actionlint` package][winget] is available in the winget-pkgs repository:

```powershell
winget install actionlint
```

## Linux

### [Arch Linux](https://archlinux.org/)

[`actionlint` package][archlinux] is available in the official repository:

```sh
pacman -S actionlint
```

Alternatively actionlint is also available on [AUR][aur]. The packages can be installed via [`paru`][paru] command.

- [actionlint-bin](https://aur.archlinux.org/packages/actionlint-bin)
- [actionlint-git](https://aur.archlinux.org/packages/actionlint-git)

### [Nix](https://nixos.wiki/)

[`actionlint` package][nixpkgs] is available in the Nix ecosystem:

On NixOS:

```sh
nix-env -iA nixos.actionlint
```

On Non NixOS:

```sh
nix-env -iA nixpkgs.actionlint
```

## macOS

### [Homebrew][homebrew]

[`actionlint`][formula] formula is provided by Homebrew officially.

```sh
brew install actionlint
```

That formula tracks the upstream project. To install this fork instead, use its own tap, which is updated automatically
on every release:

```sh
brew install kjanat/actionlint/actionlint
```

> [!WARNING]
> Since the `actionlint` executable is unsigned, macOS displays a warning and tries to move it to the Trash. To allow it to run,
> go to 'Settings -> Privacy & Security' and grant the permission.

## [npm](https://www.npmjs.com/)

[`@kjanat/actionlint`][npm-package] installs a prebuilt binary, so no Go toolchain is needed:

```sh
npm install --save-dev @kjanat/actionlint
```

Or run it without installing:

```sh
npx @kjanat/actionlint
```

The package itself carries no binary. It declares one `optionalDependencies` entry per platform, each holding a single
executable and declaring its `os` and `cpu`, so your package manager downloads only the one matching your machine. The
`actionlint` command is a small launcher that resolves that package and execs the binary inside it, forwarding the exit
status unchanged.

The binaries are the same ones attached to the [GitHub release][releases]; every archive is verified against the
release's published checksums before being repackaged.

Linux users on Alpine need nothing special: the binaries are statically linked, so the `linux-*` packages run on musl
and glibc alike.

## Prebuilt binaries

Download an archive file from [the releases page][releases] for your platform, unarchive it and put the executable file to a
directory in `$PATH`.

Prebuilt binaries are built at each releases by CI for the following OS and arch:

- macOS (x86_64, arm64)
- Linux (i386, x86_64, arm32, arm64)
- Windows (i386, x86_64, arm64)
- FreeBSD (i386, x86_64)

Note that the following targets are not tested since GitHub Actions doesn't support them:

- Linux i386, arm32
- Windows i386
- FreeBSD i386, x86_64

To install these binaries [`gh`][gh] command is useful. The following command is an example for x86_64 Linux.

```sh
gh release download --repo kjanat/actionlint --pattern '*_linux_amd64.tar.gz' v1.13.0
tar xf actionlint_1.13.0_linux_amd64.tar.gz
./actionlint -version
```

Optionally you can verify the [attestation][attestations] of the downloaded artifact. This is highly recommended in terms of
security. Note that the attestation support was introduced since actionlint v1.7.11.

```sh
gh attestation verify -R kjanat/actionlint actionlint_1.13.0_linux_amd64.tar.gz
```

<a id="download-script"></a>

## Download script

To install `actionlint` executable with one command, [the download script](../scripts/download-actionlint.bash) is available.
It downloads the latest version of actionlint (`actionlint.exe` on Windows and `actionlint` on other OSes) to the current
directory automatically. This is a recommended way if you install actionlint in some shell script.

```sh
bash <(curl https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash)
```

When you need to install specific version of actionlint, please give the version to the 1st command line argument. The following
example installs v1.13.0.

```sh
bash <(curl https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) 1.13.0
```

This script downloads `actionlint` (or `actionlint.exe` on Windows) binary to the current working directory. When you need to put
the downloaded binary to some other directory, please give the directory path to the 2nd command line argument. The following
example installs the latest version to `/usr/bin`.

```sh
bash <(curl https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) latest /usr/bin
```

For the usage of actionlint on GitHub Actions, see [the usage document](usage.md#on-github-actions).

## Docker image

See [the usage document](./usage.md#docker) to know how to install and use an official actionlint Docker image.

## Cross-platform version managers

### asdf

You can install actionlint with the [asdf version manager][asdf] using the [asdf-actionlint][asdf-plugin] plugin, which
automates the process of installing (and switching between) various versions of GitHub release binaries. With asdf already
installed, run these commands to install actionlint:

```bash
# Add actionlint plugin
asdf plugin add actionlint

# Show all installable versions
asdf list-all actionlint

# Install specific version
asdf install actionlint latest

# Set a version globally (on your ~/.tool-versions file)
asdf global actionlint latest
```

### mise

You can install actionlint with the [mise-en-place][mise] which automates the process of installing (and switching
between) various versions of GitHub release binaries. With mise already installed, run these commands to install
actionlint:

```bash
# Show all installable versions
mise ls-remote actionlint

# Install specific version
mise install actionlint@latest

# Set a version globally (on your ~/.config/mise/config.toml file)
mise use -g actionlint@latest
```

## Build from source

Recent [Go][Go] toolchain is necessary to build actionlint from source. Last two major versions of Go are supported.

```sh
# Install the latest stable version
go install actionlint.kjanat.dev/cmd/actionlint@latest

# Install the head of the main branch
go install actionlint.kjanat.dev/cmd/actionlint@master
```

---

[Checks](checks.md) | [Usage](usage.md) | [Configuration](config.md) | [Go API](api.md) | [References](reference.md)

[formula]: https://formulae.brew.sh/formula/actionlint
[homebrew]: https://brew.sh/
[releases]: https://github.com/kjanat/actionlint/releases
[gh]: https://docs.github.com/en/github-cli/github-cli/about-github-cli
[attestations]: https://docs.github.com/en/actions/concepts/security/artifact-attestations
[Go]: https://golang.org/
[asdf]: https://asdf-vm.com/
[asdf-plugin]: https://github.com/crazy-matt/asdf-actionlint
[chocolatey]: https://community.chocolatey.org/packages/actionlint
[npm-package]: https://www.npmjs.com/package/@kjanat/actionlint
[upstream]: https://github.com/rhysd/actionlint
[scoop]: https://scoop.sh/#/apps?q=actionlint&s=0&d=1&o=true
[winget]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/r/rhysd/actionlint
[archlinux]: https://archlinux.org/packages/extra/x86_64/actionlint/
[aur]: https://aur.archlinux.org/
[paru]: https://github.com/Morganamilo/paru
[nixpkgs]: https://github.com/NixOS/nixpkgs/blob/master/pkgs/development/tools/analysis/actionlint/default.nix
[mise]: https://github.com/jdx/mise
