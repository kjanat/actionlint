# PyPI wheels

This package builds the wheels of [`actionlint-kjanat`](https://pypi.org/project/actionlint-kjanat/).

Edit [`PACKAGE_README.md`](PACKAGE_README.md) to change the PyPI description. The builder replaces `{{version}}` with the release version.

```text
distribution/pypi/
  python/actionlint_kjanat/__init__.py  the launcher in every wheel
  src/cli.ts                            the build command, on @kjanat/dreamcli
  src/wheel.ts                          platform tags, metadata and wheel assembly
  src/archive.ts                        tar.gz and zip reader, zip writer
  test/wheel.test.ts                    tests
```

## Build

To build the wheels of a published release by hand:

```sh
version=1.2.3
gh release download "v${version}" --repo kjanat/actionlint --dir release --pattern "actionlint_${version}_*"
node distribution/pypi/src/cli.ts build --version "${version}" --assets release --out distribution/pypi/dist
```

`--assets` names a directory with the release archives and `actionlint_<version>_checksums.txt`. The builder checks the SHA-256 of each archive, writes one wheel per platform tag to `--out`, and refuses to overwrite a wheel. `--version` takes a stable `MAJOR.MINOR.PATCH` version. The builder needs Node 24 or later and `bun install --frozen-lockfile` from the repository root.

Use `--build 1` to correct the packaging of a published version. To publish those wheels, pass `-f wheel-build=1` to `pypi-release.yml`.

## Wheels

| Release archive | Wheel platform tags                               |
| --------------- | ------------------------------------------------- |
| `darwin_amd64`  | `macosx_<major>_<minor>_x86_64`                   |
| `darwin_arm64`  | `macosx_<major>_<minor>_arm64`                    |
| `linux_386`     | `manylinux_2_17_i686`, `musllinux_1_1_i686`       |
| `linux_amd64`   | `manylinux_2_17_x86_64`, `musllinux_1_1_x86_64`   |
| `linux_arm64`   | `manylinux_2_17_aarch64`, `musllinux_1_1_aarch64` |
| `linux_armv6`   | `manylinux_2_17_armv7l`, `musllinux_1_1_armv7l`   |
| `windows_386`   | `win32`                                           |
| `windows_amd64` | `win_amd64`                                       |
| `windows_arm64` | `win_arm64`                                       |

Every wheel is tagged `py3-none-<platform>`. The builder reads the minimum macOS version of each Darwin binary from `LC_BUILD_VERSION` or `LC_VERSION_MIN_MACOSX` and rejects a binary without either. It also rejects a minimum such as 13.3. pip matches macOS 11 and later only through `macosx_<major>_0` tags.

The glibc and musl wheels hold the same static binary. Releases publish no source distribution.

Both console scripts call `actionlint_kjanat:main`. On POSIX systems, `main` replaces the Python process with the binary through `os.execv`. On Windows, it runs the binary as a child process.

## Release automation

`.github/workflows/release-prepare.yml` attaches wheels to every prepared release.

`.github/workflows/pypi-release.yml` publishes the wheels. The `pypi` job of `release.yml` calls it after the `verify` job. Its `build` job takes one of two paths:

- **Released wheels.** The job downloads the release assets and checks them against `release-candidate.json` with `tools/release/release-candidate.mjs verify`. The job publishes the released bytes without a rebuild.
- **Wheels built from the archives.** The job checks the archives against the release checksums and builds the wheels with the composite action of the workflow commit.

The `publish` job uploads the wheels through PyPI trusted publishing from the `pypi` environment and skips files already on PyPI. PyPI must list `kjanat/actionlint`, the `pypi-release.yml` workflow and the `pypi` environment as the trusted publisher of `actionlint-kjanat`.

To publish the wheels of a release by hand, or to rerun a failed upload:

```sh
gh workflow run pypi-release.yml --repo kjanat/actionlint --ref master -f tag=v1.2.3
```

A manual run requires a published release and an annotated tag with a verified PGP signature that points at the checked-out commit.

## Tests

```sh
npm test --workspace @kjanat/actionlint-pypi
```

The tests need `python3`, `tar` and `zip` on `PATH`.
