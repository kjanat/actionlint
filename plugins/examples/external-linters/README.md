# External linter examples

This directory contains document providers for yamllint, zizmor, Poutine,
Runner Guard, Syft, Grype and Grant. Each can be packaged separately and uses
the captured workflow or action source. Repository scanners scan an isolated
temporary copy of that source rather than the user's checkout.

To use a published plugin in another repository, install its registry name:

```sh
actionlint plugin install yamllint
actionlint check --allow-config-processes .github
```

Replace `yamllint` with `zizmor`, `poutine`, `runner-guard`, `syft`, `grype` or
`grant` to install another integration. Install Node and the scanner on `PATH`.
Grype needs its vulnerability database. Run `grype db update` before its first
scan so the initial download does not use the plugin's execution time limit.

## Build from source

These packages use `@kjanat-actionlint/plugin-sdk` through `workspace:*`.
Build from the repository root:

```powershell
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
bun run --filter @kjanat-actionlint/plugin-yamllint pack
```

Select the other packages with `--filter @kjanat-actionlint/plugin-zizmor`, `--filter @kjanat-actionlint/plugin-poutine`, `--filter @kjanat-actionlint/plugin-runner-guard`, `--filter @kjanat-actionlint/plugin-syft`, `--filter @kjanat-actionlint/plugin-grype`, or `--filter @kjanat-actionlint/plugin-grant`. Packing writes the package's `actionlint-provider.json` and `actionlint.js`; it does not publish a release. Install the corresponding scanner on `PATH` separately. Native process providers selected from repository config require explicit `actionlint --allow-config-processes` consent.

Load the local provider:

```sh
actionlint check --plugin ./plugins/examples/external-linters/yamllint/actionlint-provider.json .github/workflows/ci.yml
```

Set its options in `.github/actionlint.yaml`:

```yaml
lint:
  rules:
    plugins:
      yamllint:
        level: on
        options:
          strict: true
```

The image providers parse YAML with `yaml@2.9.1`, retain scalar source positions, and reject aliases, expressions, and invalid references they cannot resolve. Syft uses the public registry source and writes one real Syft JSON SBOM per distinct image to the configured `output_dir`, named `<sha256-of-image-reference>.syft.json`. Grype accepts `sbom_dir` to consume those files; without it, Grype scans the public registry reference directly. Grant requires `sbom_dir` plus `policy_file` and applies that Grant policy to the matching Syft SBOM. These paths and policies are plugin configuration; no host policy is inferred.

Poutine and Runner Guard support workflow documents only. Zizmor and yamllint support workflows and action manifests. The tools remain separate, so users can enable only the scanners they install and configure.

[`yamllint/installation.ts`](yamllint/installation.ts) and [`zizmor/installation.ts`](zizmor/installation.ts) are standalone registry fixtures included in those packages. They run the real installed scanner through actionlint, check diagnostic source ranges, and correct the input. The package preview workflow runs the registry's pinned check action against both examples before publication, including cached execution and rejection after removing cached plugin bytes.
