# PyPI wheels

This package builds the wheels published to PyPI as
[`actionlint-kjanat`](https://pypi.org/project/actionlint-kjanat/). Each wheel
contains the release binary for one platform and installs the `actionlint`
console script. Installation does not compile source code.

Edit [`PACKAGE_README.md`](PACKAGE_README.md) to change the description shown on
PyPI. The wheel builder replaces `{{version}}` in its links with the release
version. Funding also appears in the package's project links.

```text
distribution/pypi/
  python/actionlint_kjanat/__init__.py  the launcher in every wheel
  src/cli.ts                            the build command, on @kjanat/dreamcli
  src/wheel.ts                          platform tags, metadata and wheel assembly
  src/archive.ts                        tar.gz and zip reader, zip writer
  test/wheel.test.ts                    tests
```

## Build

```sh
node distribution/pypi/src/cli.ts build --version "${version}" --assets release --out distribution/pypi/dist
```

Set `--assets` to a directory containing the release archives and GoReleaser's
`actionlint_<version>_checksums.txt` file. The builder checks each archive's
SHA-256 against that file, then extracts `actionlint` or `actionlint.exe` and
`LICENSE.txt`. It writes one wheel per platform tag to `--out`, creating that
directory if needed, and refuses to overwrite an existing wheel.

Set `--version` to the stable `MAJOR.MINOR.PATCH` release version. This also
becomes the wheel version. The builder requires Node 24 or later. Run
`bun install --frozen-lockfile` from the repository root before building. Use
`node distribution/pypi/src/cli.ts build --help` to list the flags.

To build the wheels of a published release by hand:

```sh
version=1.2.3
gh release download "v${version}" --repo kjanat/actionlint --dir release --pattern "actionlint_${version}_*"
node distribution/pypi/src/cli.ts build --version "${version}" --assets release --out distribution/pypi/dist
```

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

Every wheel is tagged `py3-none-<platform>`. For each Darwin binary, the builder
reads the minimum macOS version from its `LC_BUILD_VERSION` load command for
platform 1 (macOS). If that command is absent, it uses `LC_VERSION_MIN_MACOSX`.
The version becomes the macOS wheel tag. The builder skips `LC_BUILD_VERSION`
commands for other platforms and rejects a binary with no macOS version command.
pip matches macOS 11 and later only through `macosx_<major>_0` tags, so the
builder rejects a minimum such as 13.3.

The Linux binaries are static, so the glibc and musl wheels for an architecture
contain the same files. PyPI accepts no FreeBSD wheels; download those archives
from GitHub releases. Releases publish wheels without a source
distribution. On a platform without a matching wheel, pip reports that no
matching distribution exists.

A wheel holds the `actionlint_kjanat` package with the launcher and `bin/actionlint` (`bin/actionlint.exe` on Windows), and a `.dist-info` directory with `METADATA`, `WHEEL`, `entry_points.txt`, `licenses/LICENSE.txt` and `RECORD`. The console script `actionlint` calls `actionlint_kjanat:main`. On POSIX systems `main` replaces the Python process with the binary through `os.execv`. On Windows it runs the binary as a child process and exits with its exit code.

## Release automation

Every release prepared by `.github/workflows/release-prepare.yml` carries its wheels as release assets. The `prepare` job runs the `.github/actions/pypi-wheels` composite action twice. Before the Go tests it builds wheels from stub archives into a scratch directory. After GoReleaser it builds the real wheels into the release assets directory beside the archives. Both runs install the `manylinux_2_17_x86_64` wheel into a virtual environment and check the version that `actionlint -version` reports. The `draft` job records every wheel with its size and SHA-256 in `release-candidate.json` and uploads it with the draft. The Windows row of the `action` job installs the prepared `win_amd64` wheel and runs the same check.

`.github/workflows/pypi-release.yml` publishes the wheels. The `pypi` job of `.github/workflows/release.yml` calls it after the `verify` job, and the `action-tags` job proceeds when the `pypi` job succeeds or is skipped. Its `build` job has one step that detects whether the release carries `.whl` assets, and that step selects the source of the wheels:

- **Released wheels.** The job downloads every release asset, checks that `release-candidate.json` names the tag and its commit, and runs `tools/release/release-candidate.mjs verify` on the directory. That script checks the size and SHA-256 of every asset, the wheels included, against the manifest. The `verify` job of `release.yml` attests the wheels at publication, and a manual run verifies that build provenance for every wheel with `gh attestation verify`, so a wheel replaced on the release after publication fails even when the manifest was rewritten with it. The job then installs the `manylinux_2_17_x86_64` wheel and publishes the released bytes. Nothing is rebuilt.
- **Wheels built from the archives.** A release without wheels, v1.17.0 and earlier, takes this path. The job downloads the archives with `gh release download`, checks them with `sha256sum` against the release's checksums file, builds the wheels with the composite action from the workflow's own commit, and installs the `manylinux_2_17_x86_64` wheel. A manual run also verifies the build provenance of every archive with `gh attestation verify`, and runs `tools/release/release-candidate.mjs verify` from the tag checkout when the release carries `release-candidate.json`. Otherwise it logs a notice that the tag predates the candidate pipeline.

The `build` job uploads the wheels as the `pypi-wheels` artifact. The `publish` job uploads them with `pypa/gh-action-pypi-publish` through PyPI trusted publishing from the `pypi` environment and skips files PyPI already has. PyPI must list `kjanat/actionlint`, the `pypi-release.yml` workflow and the `pypi` environment as the trusted publisher of `actionlint-kjanat`.

To publish the wheels of a published release by hand, or to rerun a failed upload, dispatch the workflow with the tag:

```sh
gh workflow run pypi-release.yml --repo kjanat/actionlint --ref master -f tag=v1.2.3
```

A manual run checks out the tag beside the workflow's own commit before choosing a path. It requires a published GitHub release for the tag that is not a draft, and an annotated tag with a PGP signature that GitHub reports as verified, pointing at the checked-out commit. `-f dry-run=true` skips those checks and the provenance check, collects and smoke-tests the wheels, and publishes nothing.

## Tests

```sh
npm test --workspace @kjanat/actionlint-pypi
```

The tests build wheels from stub archives, check their names, `METADATA`, `WHEEL`, `entry_points.txt` and `RECORD`, and install one into a virtual environment with `python3 -m venv` and pip. They run the console script and check that the stub binary replaced the launcher process and that its exit code reaches the caller. A second run patches `os.name` to `nt` and checks the Windows branch. The tests need `python3`, `tar` and `zip` on `PATH`.
