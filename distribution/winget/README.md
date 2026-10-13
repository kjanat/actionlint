# WinGet manifests

This package generates the three manifests that publish a release as `kjanat.actionlint` in [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs): the version, installer and `en-US` locale files.

## Generate

```sh
node distribution/winget/src/cli.ts generate \
  --version 1.17.0 \
  --checksums dist/actionlint_1.17.0_checksums.txt \
  --release-notes release-notes.md \
  --out assets
```

`--checksums` is the GoReleaser checksums file of the release; the three Windows zips come from it. `--release-notes` is the Markdown body of the release; it is rendered to plain text, then cut at 10000 characters with a link to the release. `--release-date` defaults to today in UTC. `ManifestVersion` and the schema header are 1.12.0, the version `src/schema.ts` transcribes. Every flag also reads an environment variable: `VERSION`, `WINGET_CHECKSUMS`, `WINGET_RELEASE_NOTES`, `WINGET_OUT`, `WINGET_RELEASE_DATE`.

The `winget` job of the release workflow runs this after the release is published, with the verified checksums and the published release body, and submits the files to winget-pkgs with `scripts/release-assets.mjs publish-winget`.

## Validation

`src/schema.ts` holds Zod schemas transcribed from the 1.12.0 manifest schemas: every pattern, length, enum and uniqueness rule of the fields the generator emits, with unknown keys rejected. The manifest objects are typed from them and parsed through them before they are written.

## Executable validation

`kjanat.actionlint` is a ZIP containing a portable CLI, not an executable installer. Running `actionlint` with no arguments outside a Git repository exits 3 because there are no workflows to lint. That exit code is not an installation success code: `InstallerSuccessCodes` applies to installer processes, not to an installed application's own command-line behavior. Do not declare it for this package.

The manifest instead supplies `InstallationMetadata.Files` with `FileType: launch` and `InvocationParameter: -version`, which lets tools invoke the installed `actionlint.exe` successfully without requiring a repository. This metadata does not guarantee that the WinGet automated executable probe uses those arguments. If it still reports `Validation-Executable-Error` after successful installation and a working `actionlint -version`, the remaining check requires administrator review; do not change the CLI's normal exit codes just to satisfy the probe.
