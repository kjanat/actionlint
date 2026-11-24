# Browser language server

`@kjanat-actionlint/language-server` runs actionlint's Go language server in a WebAssembly worker.

## Use with a browser editor

Bundle the worker as a module worker. With Vite:

```ts
import { createBrowserLanguageClient } from '@kjanat-actionlint/language-server';
import workerURL from '@kjanat-actionlint/language-server/worker?worker&url';

const files = new Map<string, string>();
const worker = new Worker(workerURL, { type: 'module' });
const client = createBrowserLanguageClient(worker, {
  async readFile(uri) {
    return files.get(uri) ?? null;
  },
}, {
  rootURI: 'file:///workspace',
  onDiagnostics(params) {
    // Apply only publications whose version equals the editor's current document version.
    console.log(params.uri, params.version, params.diagnostics);
  },
  onError(message) {
    console.error(message);
  },
});

await client.initialize();
await client.open(
  'file:///workspace/.github/workflows/ci.yml',
  'on: push\njobs: {}\n',
  1,
);
await client.change(
  'file:///workspace/.github/workflows/ci.yml',
  editedYAML,
  2,
);
await client.stop();
```

`change` sends the whole document with an increasing version. Reject a code action whose document version differs from the current buffer. `client.connection` handles other protocol operations. `client.dispose()` terminates without a graceful shutdown.

Other bundlers must copy `worker.js`, its JavaScript imports, `wasm_exec.js` and `server.wasm` together. Serve JavaScript as modules and Wasm as `application/wasm`.

## Discover workflow and action files

`client.discover(uris, noIgnore)` selects files with actionlint's Go discoverer. Pass absolute file URIs of files, directories or glob patterns.

`filesystem` supplies directory entries, metadata and canonical paths. `gitQuery` runs read-only Git queries. Set the client's `platform` to `windows`, `darwin` or `linux`. The host must enforce its own filesystem scope and Git permissions.

## Connect a VS Code web extension

Pass the worker to [`vscode-languageclient/browser`](https://github.com/microsoft/vscode-languageserver-node/tree/main/client) directly:

```ts
import { FileSystemError, Uri, workspace } from 'vscode';
import { LanguageClient } from 'vscode-languageclient/browser';
import { ReadFileRequest } from '@kjanat-actionlint/language-server';

const folder = workspace.workspaceFolders?.[0];
if (!folder) throw new Error('Open a workspace before starting actionlint');

// Copy the package's dist files into the extension's server/ directory at build time.
const worker = new Worker(
  Uri.joinPath(context.extensionUri, 'server', 'worker.js').toString(),
  { type: 'module' },
);
const client = new LanguageClient('actionlint', 'actionlint', {
  workspaceFolder: folder,
  documentSelector: [{ language: 'yaml', pattern: '**/*.{yml,yaml}' }],
  initializationOptions: { tools: [] },
}, worker);

client.onRequest(ReadFileRequest, async ({ uri }) => {
  try {
    return new TextDecoder().decode(
      await workspace.fs.readFile(Uri.parse(uri)),
    );
  } catch (error) {
    if (error instanceof FileSystemError && error.code === 'FileNotFound') {
      return null;
    }
    throw error;
  }
});
await client.start();
```

The [VS Code web extension guide](https://code.visualstudio.com/api/extension-guides/web-extensions) explains the browser entry point and bundling. The server supports one workspace root, including `vscode-vfs` URIs, and rejects reads outside it.

## Supply external tools

Tools stay disabled until `tools` names them, or `initializationOptions.tools` in VS Code. Implement `BrowserHost.runTool` or register `RunToolRequest`. The request carries the command arguments and the script on standard input. Return the standard output of the tool.

## Scope and verification

The browser server analyzes opened YAML files and their local dependencies. The server loads no project configuration, downloads no remote actions and launches no native plugins. Hover and navigation are not implemented.

From this package directory:

```sh
mise exec -- bun run typecheck
mise exec -- bunx playwright install chromium
mise exec -- bun run test
```
