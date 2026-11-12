# @kjanat-actionlint/plugin-sdk

Use this SDK to write actionlint providers in TypeScript. Its root export defines
provider types and constructors, validates JSON values and diagnostics, converts
columns, and runs callbacks against serialized requests. These functions work in
web runtimes without accessing files, processes, stdin or stdout. Separate
subpaths provide authoring tools, fixture tests, the CLI, subprocess execution,
SARIF decoding, YAML helpers and stdio handling.

The SDK is not published to npm yet. In this repository, examples use the root Bun workspace. For local use outside the workspace, build and pack the SDK, then install that tarball in the author package:

```sh
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
cd plugins/sdk/typescript
bun pm pack --filename plugin-sdk-0.1.0.tgz
```

Copy the resulting tarball into the author package and run `bun add --dev file:./plugin-sdk-0.1.0.tgz`. A normal registry version range is not installable until publication. Pull requests also run the repository's `pkg-pr-new` package preview workflow; use the preview links it reports after that workflow completes.

For `validate`, `test`, and `pack`, use a matching actionlint CLI. The CI release-snapshot job uploads native archives and SHA-256 checksums as `actionlint-cli-preview`. Find the CI run for the SDK preview's commit, then download its CLI archives:

```sh
gh run list --repo kjanat/actionlint --workflow ci.yml --commit <preview-commit>
gh run download <run-id> --repo kjanat/actionlint --name actionlint-cli-preview --dir actionlint-host
```

Extract the archive for your operating system and CPU, then set `ACTIONLINT` to the extracted executable or pass `--actionlint` to the SDK CLI. The preview artifacts are available for seven days. To build actionlint from a source checkout, run `go build -o actionlint ./cmd/actionlint`.

## Public exports and runtimes

The package exports ESM JavaScript and TypeScript declarations at `.`, `/stdio`, `/authoring`, `/testing`, `/config`, `/files`, `/process`, `/sarif`, and `/images`. Bun is used for development and builds; tsdown produces the distributable files.

The root export has no Node imports or globals. `executeScriptCheck` and `executeDocumentCheck` use `TextEncoder` for UTF-8 byte limits, so callbacks and protocol validation can run in a browser bundle or another web runtime. That does not make the entire SDK or its CLI web-compatible: `/stdio`, `/authoring`, `/testing`, `/process`, and `/files` use native runtime facilities. `/images` and `/sarif` have their own package dependencies and should be bundled only where those dependencies are supported.

Callback providers are packaged as native programs that communicate through
stdin and stdout. Their `runtime` defaults to `node`; choose `bun` or `deno`
explicitly to use either. The SDK places `runtimeArgs` before the generated
program filename and inserts `deno run` automatically.

The bundler resolves dependencies with `bundle: { platform: 'neutral' }` by
default, independently of the selected runtime. Set
`bundle: { platform: 'node' }` when dependencies need Node/CommonJS resolution.
Choosing `browser` changes dependency resolution but still produces a process
manifest that expects native stdin and stdout. The current web host cannot run
that artifact unchanged. Choose a runtime and dependencies available on the
host that will run the provider. Bundling does not install Node, Bun, Deno or
native scanner executables.

## Interpreter and wrapper validation

Add `invocationDefinitions` to a provider to describe the command used in a
workflow's `shell:` field. These definitions register option validation; they do
not install an interpreter or execute the command. The `shell-name` rule reports
invalid options and declared option warnings. Script preparation uses the same
option model for its startup settings.

For example, a Python plugin can publish its argument grammar, and a wrapper
plugin can delegate to that grammar:

```ts
// In the Python provider:
invocationDefinitions: [{
 id: 'python-plugin/python',
 executables: ['python', 'python3'],
 language: 'python',
 options: [{ short: 'u' }],
 unknown_options: true,
}],

// In a separate wrapper provider:
invocationDefinitions: [{
 id: 'wrapper-plugin/python',
 executables: ['actions-shell', 'actions-shells'],
 prefix: ['python'],
 delegate: 'python-plugin/python',
}],
```

Both plugins must be loaded for delegation to work. A missing definition or an
option outside an explicitly incomplete grammar produces an unknown result;
this does not prove that the invocation is valid. `unknown_options: true` suits
partial grammars such as the Python example above. Omit it only when the listed
options cover the interpreter's complete argument grammar.

Use `extends` to borrow another definition's options and language while adding
or replacing options. An option can set `warning: 'Explain the consequence here.'`
to report a warning when it occurs. An unregistered command does not imply Bash.

The compiled process manifest carries these definitions in
`invocation_definitions`. A document-analysis WebAssembly component can declare
the same field in its `actionlint.plugin` metadata section. Definition IDs must
be unique across loaded plugins and built-in interpreters. Conflicting command
selectors produce an unknown result instead of selecting a plugin by load order.

## Build and test

From the repository root:

```sh
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
bun run --filter @kjanat-actionlint/plugin-sdk typecheck
bun run --filter @kjanat-actionlint/plugin-sdk test
```

The SDK test script builds `cmd/actionlint` once, then runs Bun tests against SDK
source. It removes the executable and its temporary directory when the tests finish.
Set `ACTIONLINT` to an existing actionlint executable to skip the Go build. Running
`bun test` directly does not build actionlint; for authoring integration tests, set
`ACTIONLINT` to an executable with `plugin validate/test` support.

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

Use `configurationTransform` to convert user settings before execution. The jq program receives
`{config, config_file}` and returns `{options, source_prefix?, language?, read_access?}`.
Actionlint validates user settings against `configSchema` first. Declare path options in
`configurationPaths` as JSON pointers when their values need configuration-variable expansion.

Use `preparationTransform` when preparation depends on interpreter or environment facts.
For example, this program adds a tool directive and passes the normalized options to the process:

```ts
preparationTransform: {
  language: 'jq',
  inline: '{source_prefix:"# tool-check enabled\\n", startup:.startup, config:.settings.options}',
},
```

The program can also return `variables`, `excluded` or a `skip` reason.
See the [preparation fields](../../../docs/plugins.md#local-tool-provider-manifests) for the supplied facts.

For a process-wrapper example, see [`plugins/examples/psscriptanalyzer/actionlint.plugin.ts`](../../examples/psscriptanalyzer/actionlint.plugin.ts), its [`output.jq`](../../examples/psscriptanalyzer/output.jq), and its [`manifest.test.ts`](../../examples/psscriptanalyzer/manifest.test.ts). It demonstrates a config schema, a PowerShell version probe, UTF-16 coordinates, suggested fixes, and captured output. Run its actionlint-backed tests from the repository root with `bun run --filter @kjanat-actionlint/plugin-psscriptanalyzer test`; set `ACTIONLINT` to a compatible actionlint executable.

Its [`installation.ts`](../../examples/psscriptanalyzer/installation.ts) is a separate registry fixture: it runs the installed PSScriptAnalyzer over both PowerShell shells, checks Unicode source ranges, and verifies that corrected snippets pass. Unlike recorded-output tests, it requires `pwsh` and PSScriptAnalyzer 1.25.0 or newer on the test host. The registry harness uses a fresh home and omits the caller's `PSModulePath`; install the module in a standard system module location when running that harness.

## Validate and test captured fixtures

`actionlint-plugin init ./plugins/my-check` creates a scaffold. In an existing package it preserves metadata, existing scripts, README, and the installed SDK dependency specifier. It adds `plugin:validate`, `plugin:test`, and `plugin:pack` scripts where those names are free, extends existing ignore rules, and includes the manifest in an existing package `files` list. Source/fixture collisions fail before modifying the project.

When initializing before installing a preview or local SDK, supply its dependency specifier explicitly:

```sh
actionlint-plugin init --sdk file:./plugin-sdk-0.1.0.tgz
```

Otherwise the CLI keeps an installed SDK dependency (including `workspace:*` or a preview URL); a new dependency defaults to the CLI package's version. After editing the TypeScript definition and adding fixture data, use:

```sh
actionlint-plugin validate ./plugins/my-check/actionlint.plugin.ts
actionlint-plugin test ./plugins/my-check/actionlint.plugin.ts ./plugins/my-check/actionlint.fixtures.json
actionlint-plugin pack ./plugins/my-check/actionlint.plugin.ts
```

`validate` and `test` require a kjanat/actionlint executable with `plugin validate/test` support. `pack` requires actionlint manifest validation. Select the executable with `ACTIONLINT` or `--actionlint`. `validate` and `test` accept TypeScript definitions or packed JSON manifests; `test --json` prints the machine-readable report. `pack` validates before writing provider outputs.

TypeScript definitions load through jiti in both ESM and CommonJS projects, without changing the project's module type. Loading executes the author's module code; jq fixture evaluation then runs in actionlint without launching the declared scanner. Each compilation reloads the definition and its imports without writing a transpilation cache into the author project.

`bun run --filter @kjanat-actionlint/plugin-sdk test:package` separately checks the built SDK tarball from a standalone installed consumer, including the Node CLI, fixtures, and the resulting provider tarball. Normal SDK tests run against TypeScript source.

Fixture tests record scanner stdout and request source/config; they exercise the actual actionlint schema and jq transforms without launching the wrapped scanner or accessing the network. They do not prove installed scanner compatibility. A JSON fixture case has a unique `name`, a `request` with `source` and `filename` (optional `config`), recorded `stdout`, and at least one assertion:

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

Use optional `variables` to add test variables. The `exit_code` defaults to zero
and must be accepted by the manifest. Assertions can check decoded diagnostics
with `expect`, transformed configuration with `expect_config`, the complete jq
variable map with `expect_vars`, generated source with `expect_source`, a preparation
plan with `expect_preparation`, or an expected processing failure with
`expect_error`. Actionlint rejects empty suites, duplicate names and cases without
assertions.

For providers with `preparationTransform`, include `preparation` facts in each fixture:

```ts
{
  name: 'Bash startup',
  request: { source: 'echo ok\n', filename: 'script.sh' },
  preparation: { language: 'bash', startup: 'set -e', environment_known: true },
  expect_source: '# tool-check enabled\nset -e\necho ok\n',
}
```

Actionlint supplies `source` and normalized `settings` from the fixture request. It then evaluates
the preparation program and checks any recorded scanner output against the generated source.

For typed fixtures, `createPluginTester` works with Bun, Node, Deno, Vitest, or another test runner. It loads the trusted author definition, compiles the manifest, then sends captured cases to actionlint:

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

`plugin.validate()` checks actionlint schemas and jq compilation. `plugin.test(cases)` returns case reports; `plugin.assert(cases)` throws `ProviderTestError` when validation or an assertion fails. The actionlint executable defaults to `ACTIONLINT`, then `actionlint` on PATH; subprocess timeout defaults to 30 seconds.

## Pack and distribute

`actionlint-plugin pack` writes `actionlint-provider.json`. For callback
providers it also writes a bundled `actionlint.js` in ESM packages or
`actionlint.mjs` in CommonJS packages and adds a bin entry. It adds an
`./actionlint` subpath to existing export maps. Packages without an export map
keep their existing main and subpath resolution. Existing executables are
preserved, including the single-string `bin` shorthand. Process-only
definitions embed jq without producing a callback bundle. Bundles are limited
to 512 KiB and manifests to 1 MiB.

Packing creates files for distribution. Publishing an npm package or release
asset, registering its identity and digest, and verifying compatibility with an
installed scanner are separate steps. Registry maintainers decide whether to
accept a package and its host-specific compatibility evidence.

## Other subpaths

- `/stdio` provides native `serveScriptEngine` and `serveDocumentEngine`. Each reads one request from stdin and writes a JSON response to stdout. Send logs to stderr so stdout contains only protocol JSON.
- `/authoring` compiles TypeScript definitions and embeds provider-relative jq files.
- `/testing` uses actionlint to validate manifests and fixtures.
- `/config` reads optional JSON settings with type and allowed-value checks.
- `/files` materializes captured documents in a temporary directory for file-based tools.
- `/process` runs commands without a shell and includes JSON response decoding helpers.
- `/sarif` decodes active SARIF 2.1.0 findings with a caller-supplied URI allowlist. The reader honors applied or accepted native suppressions and reports findings whose suppression requests were rejected or are under review.
- `/images` extracts static OCI references from workflow/action YAML and builds image findings.

These helpers do not install scanners, register packages, fetch images or
sandbox native tools. The source JSDoc for each subpath documents function
imports and examples.

Native suppression syntax belongs to your callback or wrapped linter: process it
from the supplied source and return active diagnostics. Actionlint applies its
own YAML/config ignores after mapping those diagnostics. Its
`forbidden-suppression` rule does not audit directives or filters inside an
external tool. See [suppression ownership](../../../docs/plugins.md#suppression-ownership)
for the contract shared by process, component and embedded plugins.
