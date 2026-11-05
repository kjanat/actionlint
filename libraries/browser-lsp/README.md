# Browser language server

`@kjanat-actionlint/language-server` runs actionlint's Go language server in a
WebAssembly worker. It uses the same analysis, diagnostics, completions and code
actions as the native server. The browser host supplies workspace files and any
enabled external tools through JSON-RPC requests.

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
    // Apply only publications matching the editor's current document version.
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
// On editor shutdown:
await client.stop();
```

`change` sends the complete document with an increasing version. Diagnostic ranges
use zero-based UTF-16 positions and exclusive ends. Code actions include document
versions; reject an edit if its version differs from the current buffer. Use
`client.connection` for other standard protocol operations. Call `client.dispose()`
to terminate immediately without waiting for a graceful shutdown.

Other bundlers must copy `worker.js`, its JavaScript imports, `wasm_exec.js` and
`server.wasm` together. Serve JavaScript as modules and Wasm as `application/wasm`.
No Node.js runtime, subprocess or analysis service is needed in the browser.

## Connect a VS Code web extension

The worker speaks standard LSP over `postMessage`. Pass it directly to
[`vscode-languageclient/browser`](https://github.com/microsoft/vscode-languageserver-node/tree/main/client),
without wrapping it in `createBrowserLanguageClient`:

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

This is integration code for a future extension, not an extension package. Declare
the extension's browser entry point and bundle against the browser client, as
described in the [VS Code web extension guide](https://code.visualstudio.com/api/extension-guides/web-extensions).
The server maps one workspace root into its in-memory host. It sends the original
URI scheme, authority and revision query back to the client, including
`vscode-vfs` workspace URIs. Open buffers override closed files read through the
host. Reads outside the initialized workspace are rejected.

## Supply external tools

Tools are disabled unless named in `tools` (or `initializationOptions.tools` for
a VS Code client). Implement `BrowserHost.runTool` or register `RunToolRequest` to
handle the enabled bundled provider IDs. The request contains the provider's
command arguments and script on standard input; return the tool's standard output.
The existing provider manifest translates that output into findings and fixes.

The host decides how to execute a tool. For example, the playground sends
ShellCheck requests to a separate Wasm worker. Tool and file requests carry
cancellation tokens. Stop pending work when cancellation is requested, and reject
failed operations so the client can display an analysis error.

## Scope and verification

The browser server analyzes opened YAML files and local dependencies supplied by
the host. It does not discover files, load project configuration, download remote
actions or launch native plugins. Completions cover the same subset as the native
server; hover and navigation are not implemented. Multiple workspace roots need
separate server instances.

From this package directory:

```sh
mise exec -- bun run typecheck
mise exec -- bunx playwright install chromium
mise exec -- bun run test
```

The package script builds the actual Go server and runs
Chromium tests for diagnostics, virtual files, cancellation, completions and
versioned quick fixes. The repository's playground tests additionally exercise the
built editor and the real ShellCheck worker. The browser tests do not establish
that a VS Code extension has been installed or tested.
