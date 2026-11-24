# WinGet manifests

This package generates the version, installer and `en-US` locale manifests that publish `kjanat.actionlint` to [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs).

## Generate

```sh
node distribution/winget/src/cli.ts generate \
  --version 1.17.0 \
  --checksums dist/actionlint_1.17.0_checksums.txt \
  --release-notes release-notes.md \
  --out assets
```

`--checksums` takes the GoReleaser checksums file of the release. `--release-notes` takes the Markdown release body. The generator cuts the notes at 10000 characters and links to the release. `--release-date` defaults to today in UTC. Each flag also reads an environment variable: `VERSION`, `WINGET_CHECKSUMS`, `WINGET_RELEASE_NOTES`, `WINGET_OUT`, `WINGET_RELEASE_DATE`.

The `winget` job of the release workflow runs the generator and submits the files with `tools/release/release-assets.mjs publish-winget`.

## Validation

`src/schema.ts` transcribes the 1.12.0 manifest schemas into Zod. The generator parses every manifest through these schemas before it writes the files.

WinGet validation launches `actionlint -version`, which the manifest declares in `InstallationMetadata.Files` with `FileType: launch`. Do not set `InstallerSuccessCodes`.
