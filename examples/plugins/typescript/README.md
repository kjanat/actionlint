# TypeScript process provider example

This package demonstrates a source check with a suggested fix. It reports trailing spaces or tabs by scanning lines and emits one-based Unicode scalar columns with exclusive ends.

From the repository root, build the SDK with `npm run build --prefix packages/plugin-sdk`, then install the example's local SDK dependency with `npm install --prefix examples/plugins/typescript`. Run `npm run pack --prefix examples/plugins/typescript` to generate the self-contained provider executable and provider manifest with embedded registry metadata. Install the package into the consuming Node project; actionlint launches `node` and imports the package export from that project's working directory. The generated npm bin is a convenience alias; actionlint does not launch npm's Windows `.cmd` shim.

The provider is a native Node process. Repository configuration selecting it requires `--allow-config-processes`. An explicitly invoked registry plugin uses `--plugin`. This example does not create release assets or publish.
