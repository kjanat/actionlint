# TypeScript process provider example

This package demonstrates a source check with a suggested fix. It reports trailing spaces or tabs by scanning lines and emits one-based Unicode scalar columns with exclusive ends.

From the repository root, run `bun install`, then `bun run --filter @kjanat-actionlint/plugin-sdk build` and `bun run --filter @kjanat-actionlint/plugin-trailing-space pack`. The example uses `@kjanat-actionlint/plugin-sdk` through `workspace:*` and shares the root lockfile. The SDK bundles the callback with tsdown and embeds the program and its dependencies in `actionlint-provider.json`. The default manifest selects Node; consumers do not need the SDK. Authoring and testing use Bun.

The provider runs as a native process. Repository configuration selecting it requires `--allow-config-processes`. An explicitly invoked registry plugin uses `--plugin`. This example does not publish automatically.

[`installation.ts`](installation.ts) is a behavioral fixture for the registry's shared check and publish actions. It installs the packaged manifest, verifies a mapped trailing-whitespace diagnostic, then corrects the source and verifies that the finding disappears. The registry harness repeats those checks offline and verifies rejection after removing cached bytes.

From a registry checkout, with this example already packed:

```sh
bun scripts/cli.ts check \
  --registry ./registry --actionlint ./actionlint \
  --artifact ../actionlint/examples/plugins/typescript/actionlint-provider.json \
  --fixture ../actionlint/examples/plugins/typescript/installation.ts \
  --kind manifest --allow-config-processes --report candidate.json
```

To package the manifest as OCI, run from this example directory:

```sh
oras push --oci-layout ./plugin-layout:example \
  --artifact-type application/vnd.actionlint.plugin.v1+json \
  actionlint-provider.json:application/vnd.actionlint.tool-provider.v1+json
oras resolve --oci-layout ./plugin-layout:example
```

Use the printed digest in the intended `oci://ghcr.io/owner/package@sha256:…` reference. In the registry check above, replace `--artifact` with `--oci "$PLUGIN_OCI_REFERENCE" --oci-layout ../actionlint/examples/plugins/typescript/plugin-layout`. The image need not be uploaded first. The receipt binds the OCI root digest; native execution still requires explicit consent. `plugin-layout/` is a generated, ignored build output.
