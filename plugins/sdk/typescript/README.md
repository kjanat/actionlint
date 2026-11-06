# @kjanat-actionlint/plugin-sdk

TypeScript APIs for authoring actionlint providers. The package root is a web-safe pure API: it defines provider types and constructors, validates JSON values and diagnostics, converts columns, and executes callbacks against serialized requests without touching files, processes, stdin, or stdout. Native authoring, fixture tests, CLI, subprocess, SARIF, YAML, and stdio helpers live in separate subpaths.

The SDK is not published to npm yet. In this repository, examples use the root Bun workspace. For local use outside the workspace, build and pack the SDK, then install that tarball in the author package:

```sh
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
cd plugins/sdk/typescript
bun pm pack --filename plugin-sdk-0.1.0.tgz
```

Copy the resulting tarball into the author package and run `bun add --dev file:./plugin-sdk-0.1.0.tgz`. A normal registry version range is not installable until publication. Pull requests also run the repository's `pkg-pr-new` package preview workflow; use the preview links it reports after that workflow completes.

For `validate`, `test`, and `pack`, use a matching actionlint CLI. The CI release-snapshot job uploads native archives and SHA-256 checksums as `actionlint-cli-preview`. Find the CI run for the SDK preview's commit, then download its host archives:

```sh
gh run list --repo kjanat/actionlint --workflow ci.yml --commit <preview-commit>
gh run download <run-id> --repo kjanat/actionlint --name actionlint-cli-preview --dir actionlint-host
```

Extract the archive for your operating system and CPU, then set `ACTIONLINT` to the extracted executable or pass `--actionlint` to the SDK CLI. Host preview artifacts are retained for seven days. From a source checkout, `go build -o actionlint ./cmd/actionlint` builds the current host instead.

## Public exports and runtimes

The package exports ESM JavaScript and TypeScript declarations at `.`, `/stdio`, `/authoring`, `/testing`, `/config`, `/files`, `/process`, `/sarif`, and `/images`. Bun is used for development and builds; tsdown produces the distributable files.

The root export has no Node imports or globals. `executeScriptCheck` and `executeDocumentCheck` use `TextEncoder` for UTF-8 byte limits, so callbacks and protocol validation can run in a browser bundle or another web runtime. That does not make the entire SDK or its CLI web-compatible: `/stdio`, `/authoring`, `/testing`, `/process`, and `/files` use native runtime facilities. `/images` and `/sarif` have their own package dependencies and should be bundled only where those dependencies are supported.

Callback providers are packaged as native stdio programs. Their `runtime` defaults to `node`; `bun` and `deno` are opt-in, and `runtimeArgs` are passed before the generated program filename (`deno run` is inserted automatically). The bundler's dependency resolution defaults to `bundle: { platform: 'neutral' }`, independently of that runtime choice. Set `bundle: { platform: 'node' }` when dependencies need Node/CommonJS resolution. `browser` is also a resolution choice, but it does not change the generated process manifest: it still expects native stdin/stdout and the current web host does not run that artifact unchanged. Select a runtime and its dependencies that the consuming host actually provides; bundling never installs Node, Bun, Deno, or native scanner executables.

## Build and test

From the repository root:

```sh
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
bun run --filter @kjanat-actionlint/plugin-sdk typecheck
bun run --filter @kjanat-actionlint/plugin-sdk test
```

The SDK test script builds the current `cmd/actionlint` host once and removes it with its temporary directory afterward. Set `ACTIONLINT` to an existing host binary to skip that Go build. It then runs Bun tests against SDK source. Direct `bun test` runs the test files without preparing the host; authoring integration tests need `ACTIONLINT` to point to a host with `plugin validate/test` support.

## Author a process provider

A process provider wraps an installed tool whose captured output can be decoded. The source and JSON output below show a small complete definition; `output.jq` maps the tool's recorded JSON into actionlint diagnostics.

```ts
// actionlint.plugin.ts
import { defineProcessProvider, jqOutput } from '@kjanat-actionlint/plugin-sdk';

export default defineProcessProvider({
  id: 'example/json-linter',
  version: '0.1.0',
  name: 'Example JSON linter',
  description: 'Adapts a JSON-producing script checker.',
  license: 'MIT',
  repository: 'https://github.com/example/actionlint-plugin-json-linter',
  shells: ['bash', 'sh'],
  entry: 'json-linter',
  args: ['--stdin-json'],
  stdin: 'request',
  config: {
    render: 'json-stdin',
    transform: { language: 'jq', inline: '{config: (. // {}), vars: {}}' },
  },
  output: { format: 'json', transform: jqOutput('./output.jq') },
  exit_codes: [0, 1],
  env_passthrough: ['PATH'],
  rule: {
    name: 'json-linter',
    category: 'correctness',
    subcategory: 'shell',
    maturity: 'stable',
    description: 'Report findings from json-linter',
    provider: 'example/json-linter',
  },
});
```

Host-provided values such as the virtual filename and exit status are available in jq as `$vars.filename` and `$vars.exit_code`; read them through `$vars`.

```jq
# output.jq: input is the scanner's JSON array; filename is supplied by actionlint.
map({
  filename: $vars.filename,
  code: .rule,
  message: .message,
  severity: .severity,
  location: {row: .line, column: .column}
})
```

`entry` names a host-installed executable. `args` are separate tokens without shell interpolation. `stdin: 'request'` sends JSON containing captured `source`, `filename`, and transformed `config`; `stdin: 'source'` sends only source text. `config.render` supports `none`, `json-stdin`, `json-file`, `toml-file`, and `toml-pairs`; `forbidden` rejects selected dotted config keys after transformation. A jq config transform typically returns `{config, vars}`. Output formats are `script-diagnostics@1`, `json`, `text`, and `sarif@2.1.0`. Arbitrary JSON uses an output jq transform; SARIF and tool JSON can declare coordinate rows, columns, and exclusive/inclusive ends when they differ from the default.

Script requests also carry `environment`: one record per possible OS, with scoped
names and separate comparisons for runner merging and shell lookup. No values
or secrets are included. Windows runner merging ignores case; Bash lookup does
not. `unknown_names` marks unresolved declarations. A name missing from the list
could still come from the runner or an earlier command through `GITHUB_ENV`.

The process manifest can also declare fixed `env`, removed `env_unset`, accepted `exit_codes`, serial `concurrency`, and a `versionProbe` with literal arguments and one captured version group. `env_passthrough` selects host environment names; it does not install tools. Declare only what the command needs (for example PATH, or `PSModulePath` for PowerShell modules). Native processes are not sandboxed by this SDK. Repository-selected native processes require `actionlint --allow-config-processes`; explicit invocation uses `--plugin`.

For a complete real-world wrapper, see [`plugins/examples/psscriptanalyzer/actionlint.plugin.ts`](../../examples/psscriptanalyzer/actionlint.plugin.ts), its [`output.jq`](../../examples/psscriptanalyzer/output.jq), and its [`manifest.test.ts`](../../examples/psscriptanalyzer/manifest.test.ts). That example shows config schema, a PowerShell version probe, UTF-16 coordinates, suggested fixes, and captured output. Run its host-backed tests from the repository root with `bun run --filter @kjanat-actionlint/plugin-psscriptanalyzer test`; set `ACTIONLINT` to a compatible host binary.

Its [`installation.ts`](../../examples/psscriptanalyzer/installation.ts) is a separate registry fixture: it runs the installed PSScriptAnalyzer over both PowerShell shells, checks Unicode source ranges, and verifies that corrected snippets pass. Unlike recorded-output tests, it requires `pwsh` and PSScriptAnalyzer 1.25.0 or newer on the test host. The registry harness uses a fresh home and omits the caller's `PSModulePath`; install the module in a standard system module location when running that harness.

## Validate and test captured fixtures

`actionlint-plugin init ./plugins/my-check` creates a scaffold. In an existing package it preserves metadata, existing scripts, README, and the installed SDK dependency specifier. It adds `plugin:validate`, `plugin:test`, and `plugin:pack` scripts where those names are free, extends existing ignore rules, and includes the manifest in an existing package `files` list. Source/fixture collisions fail before modifying the project.

When initializing before installing a preview or local SDK, supply its dependency specifier explicitly:

```sh
actionlint-plugin init --sdk file:./plugin-sdk-0.1.0.tgz
```

Otherwise an installed SDK dependency (including `workspace:*` or a preview URL) is retained; a new dependency defaults to the CLI package's version. After editing the TypeScript definition and adding fixture data, use:

```sh
actionlint-plugin validate ./plugins/my-check/actionlint.plugin.ts
actionlint-plugin test ./plugins/my-check/actionlint.plugin.ts ./plugins/my-check/actionlint.fixtures.json
actionlint-plugin pack ./plugins/my-check/actionlint.plugin.ts
```

`validate` and `test` require a kjanat/actionlint host with `plugin validate/test` support. `pack` requires host manifest validation. Select the host with `ACTIONLINT` or `--actionlint`. `validate` and `test` accept TypeScript definitions or packed JSON manifests; `test --json` prints the machine-readable report. `pack` validates before writing provider outputs.

TypeScript definitions load through jiti in both ESM and CommonJS projects, without changing the project's module type. Loading executes the author's module code; jq fixture evaluation then runs in actionlint without launching the declared scanner. Each compilation reloads the definition and its imports without writing a transpilation cache into the author project.

`bun run --filter @kjanat-actionlint/plugin-sdk test:package` separately checks the built SDK tarball from a standalone installed consumer, including the Node CLI, fixtures, and the resulting provider tarball. Normal SDK tests run against TypeScript source.

Fixture tests record scanner stdout and request source/config; they exercise the actual host schema and jq transforms without launching the wrapped scanner or accessing the network. They do not prove installed scanner compatibility. A JSON fixture case has a unique `name`, a `request` with `source` and `filename` (optional `config`), recorded `stdout`, and at least one assertion:

```json
[
  {
    "name": "one warning",
    "request": { "source": "echo $VALUE\n", "filename": "script.sh" },
    "stdout": "[{\"rule\":\"quoted-variable\",\"message\":\"Quote VALUE\",\"severity\":\"warning\",\"line\":1,\"column\":6}]",
    "expect": {
      "diagnostics": [{
        "filename": "script.sh",
        "code": "quoted-variable",
        "message": "Quote VALUE",
        "severity": "warning",
        "location": { "row": 1, "column": 6 }
      }]
    }
  }
]
```

Optional `variables` adds test variables; `exit_code` defaults to zero and must be accepted by the manifest. Assertions can check decoded `expect`, transformed `expect_config`, the full `expect_vars` jq variable map, or a genuine pipeline failure with `expect_error`. Empty suites, duplicate names, and cases without assertions are rejected.

For typed fixtures, `createPluginTester` works with Bun, Node, Deno, Vitest, or another test runner. It loads the trusted author definition, compiles the manifest, then sends captured cases to the host:

```ts
import { test } from 'bun:test';
import { createPluginTester } from '@kjanat-actionlint/plugin-sdk/testing';

const plugin = createPluginTester(
  new URL('./actionlint.plugin.ts', import.meta.url),
);

test('captured warning output maps to a finding', async () => {
  await plugin.assert([{
    name: 'quoted variable',
    request: { source: 'echo $VALUE\n', filename: 'script.sh' },
    stdout:
      '[{"rule":"quoted-variable","message":"Quote VALUE","severity":"warning","line":1,"column":6}]',
    expect: {
      diagnostics: [{
        filename: 'script.sh',
        code: 'quoted-variable',
        message: 'Quote VALUE',
        severity: 'warning',
        location: { row: 1, column: 6 },
      }],
    },
  }]);
});
```

`plugin.validate()` checks host schemas and jq compilation. `plugin.test(cases)` returns case reports; `plugin.assert(cases)` throws `ProviderTestError` when validation or an assertion fails. The host executable defaults to `ACTIONLINT`, then `actionlint` on PATH; subprocess timeout defaults to 30 seconds.

## Pack and distribute

`actionlint-plugin pack` writes `actionlint-provider.json`. Callback providers also get a bundled `actionlint.js` in ESM packages or `actionlint.mjs` in CommonJS packages and a bin entry. Existing export maps gain an `./actionlint` subpath; packages without an export map retain their existing main/subpath resolution. Existing executables are preserved, including the single-string `bin` shorthand. Process-only definitions embed jq without producing the callback bundle. Bundles are limited to 512 KiB and manifests to 1 MiB. Packing creates an author artifact; it does not publish an npm package or registry release asset, admit an identity/digest to the registry, or prove installed scanner compatibility. Registry admission and the host's compatibility evidence are separate maintainer decisions.

## Other subpaths

- `/stdio` provides native `serveScriptEngine` and `serveDocumentEngine`; stdout must remain protocol JSON.
- `/authoring` compiles TypeScript definitions and embeds provider-relative jq files.
- `/testing` offers host-backed manifest and fixture validation.
- `/config` reads optional JSON settings with type and allowed-value checks.
- `/files` materializes captured documents in a temporary directory for file-based tools.
- `/process` runs commands without a shell and includes JSON response decoding helpers.
- `/sarif` decodes active SARIF 2.1.0 findings with a caller-supplied URI allowlist. Applied/accepted native suppressions are honored; rejected or under-review requests remain findings.
- `/images` extracts static OCI references from workflow/action YAML and builds image findings.

These helpers do not install scanners, admit registry entries, fetch images, or sandbox native tools. Function-level imports and examples are documented in the subpath source JSDoc.

Native suppression syntax belongs to your callback or wrapped linter: process it
from the supplied source and return active diagnostics. Actionlint applies its
own YAML/config ignores after mapping those diagnostics. Its
`disallow-suppressions` policy does not audit directives or filters inside an
external tool. See [suppression ownership](../../../docs/plugins.md#suppression-ownership)
for the contract shared by process, component and embedded plugins.
