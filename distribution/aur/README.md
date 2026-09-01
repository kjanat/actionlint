# AUR packaging

Three packages on the [AUR](https://aur.archlinux.org/):

| Package                 | Builds                            | Arches                  |
| ----------------------- | --------------------------------- | ----------------------- |
| `actionlint-kjanat`     | from the tagged source tarball    | x86_64, aarch64, armv7h |
| `actionlint-kjanat-bin` | prebuilt from GitHub release tars | x86_64, aarch64, armv7h |
| `actionlint-kjanat-git` | from the tip of `master`          | x86_64, aarch64, armv7h |

All three install `/usr/bin/actionlint`, so each `provides` and `conflicts` the
other two as well as the `actionlint` package in the official repositories.
Install exactly one.

## Why the `-kjanat` suffix

This is a fork. `actionlint` is already in the official `extra` repository and
`actionlint-bin` and `actionlint-git` on the AUR already track
[rhysd/actionlint][upstream], all maintained by other people. Publishing under
those names would collide with theirs, so the fork carries its own.

## What they install

Beyond the binary, every package ships the rendered man page, the `docs/`
Markdown, and shell completions for bash, zsh and fish into the system autoload
directories, so no `eval` line in a user's rc file is needed.

PowerShell has no system autoload directory on Linux. The pwsh script goes to
`/usr/share/actionlint/actionlint.ps1` for users to dot-source from their
`$PROFILE`:

```powershell
if (Test-Path /usr/share/actionlint/actionlint.ps1) { . /usr/share/actionlint/actionlint.ps1 }
```

ShellCheck and Pyflakes are `optdepends`: actionlint uses them to check `run:`
scripts when they are on `PATH`, and works without them.

### Prebuilt versus source

The release binaries are built with `CGO_ENABLED=0` and are statically linked,
so `actionlint-kjanat-bin` has no runtime `depends` at all. The two source
packages build with cgo and `-linkmode=external`, per the [Arch Go packaging
guidelines][gopkg], and therefore link against the system libc.

## Automation

`.github/workflows/aur-release.yaml` publishes all three on every
`release: published` event, and on a manual `workflow_dispatch` with a `tag`
input. Per release it:

1. Rewrites each `PKGBUILD` with `.github/actions/aur-prepare`, which sets
   `pkgver` to the release version and `pkgrel` back to 1.
2. For `-bin`, injects the per-arch `sha256sums_*` read from the release's
   published checksums manifest. `updpkgsums` cannot do this, because on an
   x86_64 runner it only hashes the sources matching the host `$CARCH` and
   would silently leave `sha256sums_aarch64` and `sha256sums_armv7h` alone.
   For the source package the deploy action runs `updpkgsums` instead, its one
   source being arch-independent.
3. For `-git`, stamps the `pkgver` that `git describe` yields at that tag. Its
   real version is computed by `pkgver()` at build time, so this only decides
   what the AUR web page displays.
4. Pushes via [`KSXGitHub/github-actions-deploy-aur`], which regenerates
   `.SRCINFO` and commits to `ssh://aur@aur.archlinux.org/<pkgname>.git`.

The checked-in values are a reference snapshot; CI overwrites them before
pushing, with no hand bumping needed. `aur-prepare` fails loudly if a
`PKGBUILD` no longer contains a line it expects to rewrite; an unrewritten
`PKGBUILD` carries the previous release's version.

## Validation

Cut a release as usual, or dry-run first:

- **Without pushing**: Actions → `AUR release` → Run workflow, set `tag` and
  tick `dry-run`. This prepares and prints the finalized `PKGBUILD`s without
  touching the AUR.
- **Locally** (on an Arch box, from the repository root):

  ```bash
  cd distribution/aur/actionlint-kjanat-bin \
    && updpkgsums \
    && makepkg --printsrcinfo >/dev/null \
    && namcap PKGBUILD
  ```

The rewrite logic has its own tests, run against these `PKGBUILD` files to keep
the two in step:

```bash
cd .github/actions/aur-prepare && go test ./...
```

[`KSXGitHub/github-actions-deploy-aur`]: https://github.com/KSXGitHub/github-actions-deploy-aur
[gopkg]: https://wiki.archlinux.org/title/Go_package_guidelines
[upstream]: https://github.com/rhysd/actionlint
