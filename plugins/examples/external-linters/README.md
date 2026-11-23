# External linter examples

This directory contains providers for yamllint, zizmor, Poutine, Runner Guard, Syft, Grype and Grant. Poutine and Runner Guard check workflows only. The others also check action manifests.

Each plugin needs Node and its scanner on `PATH`. Run `grype db update` before the first Grype scan. Otherwise the database download counts against the plugin's time limit.

## Build from source

Build from the repository root:

```powershell
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
bun run --filter @kjanat-actionlint/plugin-yamllint pack
```

Packing writes the package's `actionlint-provider.json` and `actionlint.js`. Load the local provider:

```sh
actionlint check --plugin ./plugins/examples/external-linters/yamllint/actionlint-provider.json .github/workflows/ci.yml
```

Syft writes one Syft JSON SBOM per image to `output_dir`, named `<sha256-of-image-reference>.syft.json`. Grype reads those files from `sbom_dir`, or scans the registry image when `sbom_dir` is unset. Grant requires `sbom_dir` and `policy_file`.
