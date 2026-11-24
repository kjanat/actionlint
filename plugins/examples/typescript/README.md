# TypeScript process provider example

This package reports trailing spaces and tabs and suggests a fix.

From the repository root, run `bun install`, then `bun run --filter @kjanat-actionlint/plugin-sdk build` and `bun run --filter @kjanat-actionlint/plugin-trailing-space pack`. Packing embeds the program and its dependencies in `actionlint-provider.json`. The manifest runs under Node, and users do not need the SDK.

The provider runs as a native process. A repository configuration that selects it requires `--allow-config-processes`.

[`installation.ts`](installation.ts) is the registry fixture. To run the registry check, pack this example and run the following command from a registry checkout:

```sh
bun scripts/cli.ts check \
  --registry ./registry --actionlint ./actionlint \
  --artifact ../actionlint/plugins/examples/typescript/actionlint-provider.json \
  --fixture ../actionlint/plugins/examples/typescript/installation.ts \
  --kind manifest --allow-config-processes --report candidate.json
```

To package the manifest as OCI, run from this example directory:

```sh
oras push --oci-layout ./plugin-layout:example \
  --artifact-type application/vnd.actionlint.plugin.v1+json \
  actionlint-provider.json:application/vnd.actionlint.tool-provider.v1+json
oras resolve --oci-layout ./plugin-layout:example
```
