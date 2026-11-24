# @kjanat-actionlint/plugin-sdk

The SDK is unpublished. Outside this repository's Bun workspace, build and pack it:

```sh
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
cd plugins/sdk/typescript
bun pm pack --filename plugin-sdk-0.1.0.tgz
```

Copy the tarball into the author package and run `bun add --dev file:./plugin-sdk-0.1.0.tgz`. Each pull request's `pkg-pr-new` report also links a preview package.

`validate`, `test` and `pack` need a matching actionlint CLI. Download the `actionlint-cli-preview` archives from the CI run for the preview's commit. They expire after seven days.

```sh
gh run list --repo kjanat/actionlint --workflow ci.yml --commit <preview-commit>
gh run download <run-id> --repo kjanat/actionlint --name actionlint-cli-preview --dir actionlint-host
```

Set `ACTIONLINT` to the extracted executable or pass `--actionlint`. From a source checkout, run `go build -o actionlint ./cmd/actionlint`.

## Author a process provider

A process provider wraps an installed tool:

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

The jq program reads the virtual filename and exit status from `$vars.filename` and `$vars.exit_code`.

```jq
map({
  filename: $vars.filename,
  code: .rule,
  message: .message,
  severity: .severity,
  location: {row: .line, column: .column}
})
```

The manifest also accepts `env`, `env_unset`, `exit_codes`, `concurrency` and a `versionProbe`. `env_passthrough` copies named host variables, such as `PATH` or `PSModulePath`. The SDK does not sandbox native processes. A repository config that selects one requires `actionlint --allow-config-processes`.

`configurationTransform` converts user settings after actionlint validates them against `configSchema`. Its jq program receives `{config, config_file}` and returns `{options, source_prefix?, language?, read_access?}`. List path options as JSON pointers in `configurationPaths` to expand configuration variables in them.

`preparationTransform` adjusts preparation from interpreter or environment facts:

```ts
preparationTransform: {
  language: 'jq',
  inline: '{source_prefix:"# tool-check enabled\\n", startup:.startup, config:.settings.options}',
},
```

The program can also return `variables`, `excluded` or a `skip` reason. The [preparation fields](../../../docs/plugins.md#local-tool-provider-manifests) list the supplied facts.

[`plugins/examples/psscriptanalyzer`](../../examples/psscriptanalyzer/actionlint.plugin.ts) is a complete process wrapper. Run its tests with `bun run --filter @kjanat-actionlint/plugin-psscriptanalyzer test`. Its registry fixture needs `pwsh` and PSScriptAnalyzer 1.25.0 or newer in a standard system module location.

## Validate and test captured fixtures

`actionlint-plugin init ./plugins/my-check` creates a scaffold. Before the SDK is installed, pass its specifier:

```sh
actionlint-plugin init --sdk file:./plugin-sdk-0.1.0.tgz
```

Then run:

```sh
actionlint-plugin validate ./plugins/my-check/actionlint.plugin.ts
actionlint-plugin test ./plugins/my-check/actionlint.plugin.ts ./plugins/my-check/actionlint.fixtures.json
actionlint-plugin pack ./plugins/my-check/actionlint.plugin.ts
```

`validate` and `test` accept TypeScript definitions or packed manifests. `test --json` prints a machine-readable report. Loading a TypeScript definition executes its code.

Fixture tests replay recorded scanner stdout without launching the scanner. Each case has a unique `name`, a `request`, recorded `stdout` and at least one assertion:

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

The assertions are `expect`, `expect_config`, `expect_vars`, `expect_source`, `expect_preparation` and `expect_error`. `exit_code` defaults to zero and must be one the manifest accepts. Cases can add `variables`.

Providers with `preparationTransform` need `preparation` facts in each fixture:

```ts
{
  name: 'Bash startup',
  request: { source: 'echo ok\n', filename: 'script.sh' },
  preparation: { language: 'bash', startup: 'set -e', environment_known: true },
  expect_source: '# tool-check enabled\nset -e\necho ok\n',
}
```

`createPluginTester` runs typed fixtures under any test runner:

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

`plugin.validate()` checks schemas and jq compilation. `plugin.test(cases)` returns reports. `plugin.assert(cases)` throws `ProviderTestError` on failure. The tester uses `ACTIONLINT`, then `actionlint` on `PATH`, with a 30-second timeout.

## Pack

`actionlint-plugin pack` writes `actionlint-provider.json`. For callback providers it also writes `actionlint.js`, or `actionlint.mjs` in CommonJS packages, adds a bin entry and adds an `./actionlint` subpath to an existing export map. Bundles may be at most 512 KiB and manifests at most 1 MiB.

## Runtimes

Callback providers run as native processes. `runtime` defaults to `node` and also accepts `bun` and `deno`. The SDK places `runtimeArgs` before the program filename.

The bundler uses `bundle: { platform: 'neutral' }` by default. Set `bundle: { platform: 'node' }` when dependencies need Node or CommonJS resolution.

## Interpreter and wrapper validation

Add `invocationDefinitions` to describe the command in a workflow's `shell:` field. The `shell-name` rule then reports invalid options and declared option warnings. A wrapper can delegate to another plugin's grammar:

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

Delegation needs both plugins loaded. Set `unknown_options: true` unless the listed options cover the interpreter's complete grammar. `extends` borrows another definition's options and language. An option can set `warning` text to report when it occurs.

Definition IDs must be unique across loaded plugins and built-in interpreters. Wasm components declare the same `invocation_definitions` field in their `actionlint.plugin` metadata.

## Subpaths

The root export runs in browsers and other web runtimes. `/stdio`, `/authoring`, `/testing`, `/process` and `/files` need a native runtime.

- `/stdio` provides `serveScriptEngine` and `serveDocumentEngine`. Each reads one request from stdin and writes a JSON response to stdout. Send logs to stderr.
- `/authoring` compiles TypeScript definitions and embeds jq files.
- `/testing` validates manifests and fixtures with actionlint.
- `/config` reads optional JSON settings with type and allowed-value checks.
- `/files` writes captured documents to a temporary directory for file-based tools.
- `/process` runs commands without a shell and decodes JSON responses.
- `/sarif` decodes active SARIF 2.1.0 findings with a URI allowlist. It drops findings with applied or accepted suppressions.
- `/images` extracts static OCI references from workflow and action YAML.

Your callback or wrapped linter handles its own native suppression syntax and returns active diagnostics. actionlint applies its YAML and config ignores afterwards. See [suppression directives](../../../docs/plugins.md#suppression-directives).

## Build and test

From the repository root:

```sh
bun install
bun run --filter @kjanat-actionlint/plugin-sdk build
bun run --filter @kjanat-actionlint/plugin-sdk typecheck
bun run --filter @kjanat-actionlint/plugin-sdk test
```

The test script builds `cmd/actionlint` unless `ACTIONLINT` names an executable. A bare `bun test` builds nothing and needs `ACTIONLINT` for the integration tests. `bun run --filter @kjanat-actionlint/plugin-sdk test:package` checks the built tarball from a standalone consumer.
