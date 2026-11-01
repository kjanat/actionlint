# External linter examples

Seven independently packable document providers: yamllint, zizmor, Poutine, Runner Guard, Syft, Grype, and Grant. Each package uses the captured workflow or action source. Repository scanners receive an isolated temporary copy, never a scan of the user's checkout.

These packages belong to the root workspace and use `@kjanat-actionlint/plugin-sdk` through `workspace:*`. Install and build from the repository root:

```powershell
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
bun run --filter @kjanat-actionlint/plugin-yamllint pack
```

Select the other packages with `--filter @kjanat-actionlint/plugin-zizmor`, `--filter @kjanat-actionlint/plugin-poutine`, `--filter @kjanat-actionlint/plugin-runner-guard`, `--filter @kjanat-actionlint/plugin-syft`, `--filter @kjanat-actionlint/plugin-grype`, or `--filter @kjanat-actionlint/plugin-grant`. Packing writes the package's `actionlint-provider.json` and `actionlint.js`; it does not publish a release. Install the corresponding scanner on `PATH` separately. Native process providers selected from repository config require explicit `actionlint --allow-config-processes` consent.

Load the local provider:

```sh
actionlint check --plugin ./examples/plugins/external-linters/yamllint/actionlint-provider.json .github/workflows/ci.yml
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
