# Tool integrations through manifests

This design records a review of [actionlint at `f8d5a6c3`](https://github.com/kjanat/actionlint/tree/f8d5a6c3d5dd96f893a1a55ca8897ec962427aa7).
Code references describe that revision. Claims based on files the reviewer did
not read are marked **inferred** or **investigator-cited**.

## Implementation at the reviewed revision

- The checking code calls tools through `tools.Registration`, defined at `internal/checking/scripts/contract/tools.go:34-52`. It contains `Enabled`, `Required`, `Build`, `NeedsProcess`, `WithBackend`, `WithAnalyzer`, `ConfiguredBackend`, `PrepareInput` and `InputSelectors`. Composition calls `Enabled`, `Required` and `Build` at `internal/checking/composition.go:28-44`. The package documentation at `tools.go:1` states that callers need not know tool names or execution methods.

- `core.Engine`, `component.Plugin` and `ruff.ProcessBackend` implement `analyzer.ScriptBackend.Check(ctx, ScriptRequest) ([]ScriptDiagnostic, error)`. See `analyzer/backend.go:16-18,117-121,139-149` and `internal/checking/scripts/contract/ruff/backend_process.go:88`. Native ShellCheck converts backend diagnostics into its private json1 structure at `internal/checking/scripts/contract/shellcheck/analysis_shellcheck_run.go:288-297`.

- `backend.runner.Analyze` converts `ScriptDiagnostic` results to `tools.Finding` at `internal/plugins/execution/scripts/backend.go:98-212`. It selects scripts, checks template effects and filenames, filters findings, maps positions and supplies fallback identifiers. The fallback is also implemented at `internal/checking/scripts/contract/ruff/adapter.go:141-145` and `analysis_shellcheck_run.go:261-267`.

- `backend.Process` and `processBackend.Check` run a native process and decode its `{ok,diagnostics,errors}` response at `internal/plugins/execution/scripts/process.go:19-71`. Only `internal/checking/tools_registry_test.go:24,38` call this code at the reviewed revision.

- The artifact cache stores bytes under `CacheDir/<sha256hex>`, verifies their hash on reads and publishes files atomically. See `internal/plugins/artifacts/resolve.go:93,251-257,312-335`. `Import(reader, digest)` accepts verified bytes at `resolve.go:188-208`, and `sha256://` selects cached bytes at `resolve.go:157-162`. Native binaries need additional support because `parseSource` accepts only `file`, `http`, `https` and `sha256` at `resolve.go:156-183`, and `Sniff` rejects non-Wasm files at `inspect.go:48-49`.

- Plugin pins and lock files do not have fields specific to individual tools. `Pin` records `URL`, `SHA256`, `Kind`, `Contract`, `Alias` and `PluginID` at `internal/plugins/installation/model.go:119-141`. Lock version 1 records `schema_version` and `plugins` at `lock.go:24-27`.

- Tool-specific names occur in these configuration, registration and execution settings: fixed `ToolsConfig{Ruff, Shellcheck}` (`internal/input/configuration/config.go:419-424`). `if name == "ruff"` switches (`internal/checking/scripts/contract/selection/module.go:22-25`, `process.go:27-30`, `requirements.go:69-72,149-152`); fixed `adapters.Options` fields (`internal/checking/scripts/contract/adapters/registry.go:16-34`); `NormalizePaths` literal list (`internal/input/configuration/tools/command.go:110`); `normalizeToolSwitch` literal list (`internal/input/configuration/shellcheck.go:76`); catalog entries (`internal/input/configuration/catalog.go:63,105`; `internal/checking/registry/index.go:22,55,78`); CLI flags (`internal/cli/root.go:241-255`, `env.go:87-120`); plugin language table (`internal/cli/plugin.go:72-77`); default plugin injection (`internal/localapp/config_migration.go:39-42`); cache plan (`internal/localapp/plugin_cache.go:40-43`); public API map (`analysis/analysis.go:61-62,162,189-193`); provenance enum (`internal/results/finding/unavailable_source.go:19-20,36-48`); managed contract literal (`internal/checking/scripts/contract/shellcheck/document_plugin.go:24`). `rg` counts 46 further literal `"ruff"|"shellcheck"` lines in production Go outside the two tool packages and `internal/input/configuration`.

- `go.mod:7-32` and `go.sum` contain no jq, CEL or JSONPath library. Other relevant dependencies are `go-toml/v2` at `go.mod:19`, `santhosh-tekuri/jsonschema/v6` at `go.mod:21`, and `encoding/json/v2` through Go 1.27, described at `docs/design/json-processing.md:3-5`.

- At the reviewed revision, `git tag --list 'v2*'` returned no tags and `git describe` returned `v1.17.0`. Existing v2 configuration came from prerelease builds and documentation examples at `docs/config.md:283-296,345-361`.

- Dependency tests restrict where code can live. `internal/input/configuration`, `internal/plugins/{core,component}` and `plugin` must not transitively import `internal/checking/passes`, `internal/cli`, `os/exec`, `analysis` or `config`. See `internal/architecture/contracts_test.go:12-52`. `internal/checking/passes`, `internal/checking` and `analysis` must not import `internal/plugins/execution/component`, wazero or wacogo, as checked at `contracts_test.go:140-151`. The proposal therefore puts manifest and process code under `internal/checking/scripts/contract/`. `localapp` creates Wasm instances through `tools.Context.LoadEngine` at `tools.go:24`, following `docs/design/application-boundary.md:31-35`.

- The Rust design [moves external-tool integrations to extensions](https://github.com/kjanat/actionlint/blob/a75c707c06c32c0773df1d4bc7f6c9af8cfd6b45/docs/adr/0008-no-tool-specific-code.md#decision) and [excludes extension distribution from that decision](https://github.com/kjanat/actionlint/blob/2af1c3506a6034116bd2adb0f5106ec44d682474/docs/adr/0009-extension-contract.md#open). This Go design proposes manifests that a Rust host could also read.

## Proposed manifests

The manifest is a JSON file. It tells actionlint which linter to run, where to get
its executable or Wasm module and which arguments to pass. It also describes how
to convert linter output into diagnostics. Actionlint verifies downloaded files
against the SHA-256 hashes listed in the manifest.

The manifest also selects how actionlint builds the script sent to the linter.
For shell scripts, actionlint replaces `${{ ... }}` expressions with placeholders
and adds shell settings and directives. It records where it changed the script
so it can map diagnostics and fixes back to the original YAML. Python scripts
use replacements chosen for Python syntax. The linter receives the resulting script.

One registration function reads each manifest and connects the linter to these
operations through `tools.Registration`.

### What actionlint does with a manifest

| Step                                | Operation                                                                                                                                                                                                                                      | Code at the reviewed revision                                                                                                                                                                                                                                                                                                                                                                                         |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| L0. Download and cache artifacts    | Store bytes by SHA-256. Read local paths, file URLs, digest-pinned HTTPS URLs, sha256:// references and OCI digests. Write executable files from verified cached bytes.                                                                        | `internal/plugins/artifacts/resolve.go:68-127,188-208,248-335`, `inspect.go:47-96`                                                                                                                                                                                                                                                                                                                                    |
| L1. Read tool manifests             | Read tool identity, rules, execution method, platform-specific artifact digests, script selectors, requested access, command arguments, output conversion, configuration schema and version checks.                                            | core `Metadata` (`internal/plugins/execution/module/engine.go:79-111`), component static metadata (`internal/plugins/execution/component/script_engine.go:404-464`), `actionlint.plugin` section (`inspect.go:16`), plus everything currently hard-coded in `ruff/{command,native_options,compatibility,template_effects,diagnostic}.go` and `shellcheck/{analysis_shellcheck_run,input_options,shellcheck_paths}.go` |
| L2. Call the tool                   | Use the selected execution method: a native process, a core Wasm module, a Wasm component or an embedded Go callback.                                                                                                                          | `backend/process.go:41-71`, `ruff/backend_process.go:88-172`, `shellcheck` `externalCommand` (`analysis_shellcheck_run.go:300-310`), `analyzerhost.EmbeddedCommand` (`internal/integrations/process/process.go:162-164`)                                                                                                                                                                                              |
| L3. Build the script for the linter | Replace workflow expressions, determine the language dialect or target version, add directives and startup commands, and record changed lines. Supply command variables and file-access requests. Filter diagnostics caused by generated text. | shell: `shellcheck_shell.go`, `shellcheck_script.go:44-113`, `prepared_plugin.go` (= `schemas/shell-preparation-0.2.0.md:1-35`); python: `ruff/checker.go` sanitize/`templateMasks`/python-shell detection (investigator: 226-340, 369-431, 490-642); generic mask: `backend/backend.go:227-251`                                                                                                                      |
| L4. Report findings in the YAML     | Validate tool diagnostics and translate their locations and fixes to the YAML. Report findings in files the tool read. Format messages and pass through severity.                                                                              | `backend/backend.go:98-212` + `shellcheck_fixes.go:30-92` + `shellcheck_sources.go` + the two duplicated fallbacks                                                                                                                                                                                                                                                                                                    |
| L5. Register the tool               | Construct tools.Registration from the manifest, execution method, script processing and selection settings. Apply this precedence: explicit selection, repository backend, selected plugin, bundled default.                                   | `adapters/registry.go`, `selection/*`, `ruff/registry.go`, `shellcheck/registry.go`, `localapp/check.go:99-144`, `application/tool_integrations.go:18-83`                                                                                                                                                                                                                                                             |
| L6. Apply rule settings             | Apply suppression and severity settings by rule name. This behavior is unchanged.                                                                                                                                                              | unchanged                                                                                                                                                                                                                                                                                                                                                                                                             |

### Manifest fields

```go
// internal/plugins/manifests/manifest.go. Data types import analyzer, rule and encoding/json.
package provider

type Role string      // "script" | "document" | "runner"
type Transport string // "process" | "wasm-core" | "wasm-component" | "embedded"

type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`              // package identity (registry/lock), e.g. github.com/kjanat/actionlint-plugin-ruff
	Version       string          `json:"version"`
	Role          Role            `json:"role"`
	Rule          rule.Descriptor `json:"rule"`            // Rule.Name is THE tool id: tools.<name>, lint.rules.external.<name>, --tool <name>
	Scope         string          `json:"scope"`           // "source" | "composite" (today config/catalog.go:63,105)
	Transport     Transport       `json:"transport"`
	Artifacts     []Artifact      `json:"artifacts,omitempty"` // per platform; empty => PATH-discovered binary
	Preparation   string          `json:"preparation"`     // selects how actionlint builds the linter's script, see L3
	Selectors     Selectors       `json:"selectors"`
	Capabilities  Capabilities    `json:"capabilities"`
	ConfigSchema  json.RawMessage `json:"config_schema,omitempty"` // validates tools.<name>.config
	Process       *ProcessSpec    `json:"process,omitempty"`       // transport == process
	Integration   *Integration    `json:"integration,omitempty"`   // managed document-analyzer contract (deprecated route, see knot 4)
}

type Artifact struct {
	Platform  string `json:"platform"`  // "linux/amd64", "windows/arm64", "wasm32/wasip2", "wasm32/core"
	Digest    string `json:"digest"`    // "sha256:<64 hex>" = OCI blob digest = CAFS key
	Filename  string `json:"filename"`  // materialized name (keeps .exe on Windows)
	Size      int64  `json:"size"`
	MediaType string `json:"media_type,omitempty"`
}

type Selectors struct {
	Shells []string               `json:"shells,omitempty"` // canonical basenames, as backend.go:253-260 computes
	Inputs []analyzer.ScriptInput `json:"inputs,omitempty"` // analyzer/backend.go:50-54
}

type Capabilities struct {
	TemplateContract string   `json:"template_contract,omitempty"` // analyzer.TemplateEffectsContract
	Fixes            bool     `json:"fixes"`
	ReportedSources  bool     `json:"reported_sources"`            // may report findings in files it read (ShellCheck -x)
	Resources        []string `json:"resources,omitempty"`         // "workspace" | "directory" (analyzer/tool_integration.go:26-31)
	MessageTemplate  string   `json:"message_template,omitempty"`  // "{code}", "{severity}", "{row}", "{column}", "{message}"; row/column are post-remap script coordinates
}

type ProcessSpec struct {
	Entry           string            `json:"entry"`                      // artifact filename, or bare command on PATH
	Args            []string          `json:"args"`                       // literal tokens + closed placeholder grammar (below)
	Stdin           string            `json:"stdin"`                      // "source" | "request"  (request = JSON ScriptRequest, today's envelope route)
	Env             map[string]string `json:"env,omitempty"`
	EnvPassthrough  []string          `json:"env_passthrough,omitempty"`  // ambient names copied into the scrubbed child env
	ExitCodes       []int             `json:"exit_codes,omitempty"`       // accepted when stdout non-empty; default [0]
	Config          ConfigRendering   `json:"config"`
	ConfigFiles     []string          `json:"config_files,omitempty"`     // e.g. [".shellcheckrc","shellcheckrc"]; discovered upward only when policy allows
	Version         *VersionProbe     `json:"version,omitempty"`
	Output          Output            `json:"output"`
	TemplateEffects map[string]analyzer.ScriptTemplateEffect `json:"template_effects,omitempty"` // code -> effect; honoured only when probed version == Version.TemplateEffects
}

type ConfigRendering struct {
	Transform *Transform `json:"transform,omitempty"` // jq: host config JSON -> engine config JSON
	Render    string     `json:"render"`              // "none" | "json-file" | "toml-file" | "toml-pairs" | "json-stdin"
}
type Output struct {
	Format    string     `json:"format"` // "script-diagnostics@1" (envelope) | "sarif@2.1.0" | "json" | "text"
	Transform *Transform `json:"transform,omitempty"`
}
type Transform struct {
	Language string `json:"language"`         // "jq"
	Digest   string `json:"digest,omitempty"` // SHA-256 digest of the transform program
	Inline   string `json:"inline,omitempty"` // bundled manifests only
}
type VersionProbe struct {
	Args            []string `json:"args"`
	Pattern         string   `json:"pattern"`                    // RE2, one capture group
	Minimum         string   `json:"minimum,omitempty"`
	TemplateEffects string   `json:"template_effects,omitempty"` // exact version the TemplateEffects table was reviewed against
}
```

The proposed command arguments use these placeholders:

- `{var:NAME}` inserts one variable value.
- `{flag:NAME:--opt}` emits `--opt value` when script processing supplies `NAME`.
- `{switch:NAME:--on:--off}` selects one of two flags.
- `{list:NAME:SEP}` joins a list with the selected separator.
- `{config:toml-pairs:--config}` repeats `--config "k" = <inline TOML>`. This matches the conversion in `native_options.go:19-44`, cited by an investigator.
- `{config:toml-file}` and `{config:json-file}` insert a generated configuration file's path.

Actionlint supplies values such as `dialect`, `target_version`, `exclude`, `filename`, `config_file` and `external_sources` when it builds the script and command. Placeholders do not evaluate arbitrary expressions.

For a JSON transform, `.` contains parsed stdout. For a text transform, it contains the stdout string. `$vars` contains the command variables and `exit_code`.

A transform returns `{"diagnostics":[...], "sources":[...], "errors":[...]}` or a diagnostics array. Diagnostics use `analyzer.ScriptDiagnostic` with the proposed `source` and `fixes` fields below.

Actionlint validates transformed output with `jsonschema/v6`, listed at `go.mod:21`. It applies the component host's limits of 8 MiB and 10000 diagnostics. Those limits were cited by an investigator at `script_engine.go:134-229`.

### Proposed Go result fields

```go
// Proposed additions to analyzer/backend.go
type ScriptDiagnostic struct {
	/* existing fields analyzer/backend.go:140-148 */
	Source string      `json:"source,omitempty"` // engine-read file, relative to the run directory; finding is not in the snippet
	Fixes  []ScriptFix `json:"fixes,omitempty"`
}
type ScriptFix struct {
	Description  string              `json:"description,omitempty"`
	Replacements []ScriptReplacement `json:"replacements"`
}
type ScriptReplacement struct { // = shellcheck json1 replacement shape, shellcheck_fixes.go:17-23
	Row, EndRow, Column, EndColumn int
	Text                           string
}
```

```go
// internal/plugins/manifests does not import Wasm runtimes. See contracts_test.go:140-151.
func Parse(data []byte) (Manifest, error)                 // schema-validated, jsonv2 (duplicate keys rejected)
func Register(m Manifest, backend analyzer.ScriptBackend, profile Profile, sel Selection) (tools.Registration, error)

type Selection struct {              // replaces internal/checking/scripts/contract/selection and the Optional/IgnoreConfig dance in processconfig.Options:15-22
	Explicit *processconfig.Options // CLI/API/env; nil = manifest defaults
	Disabled bool
	Required bool                    // explicit selection => Build errors fatal (composition.go:34-37 semantics kept)
}

// internal/plugins/execution/preparation builds scripts and options for linters.
type Profile interface {
	ID() string                                            // "kjanat:actionlint/shell-preparation@0.3.0" | ".../python-preparation@0.1.0" | ".../expression-mask@0.1.0"
	Prepare(script tools.Script, config json.RawMessage) (Prepared, error)
	Preparer() analyzer.ScriptPreparer                     // analyzer/tool_integration.go:79. Returns nil when no script preparer is needed.
}
type Prepared struct {
	tools.PreparedInput                   // tools.go:56-63 unchanged
	Vars      map[string]string           // argv variables
	Resources []analyzer.ToolResourceRequest
}

// Generic native-process execution in internal/plugins/execution/process
type Backend struct{ /* spec, *analyzerhost.Command, Decoder, limits, probe cache */ }
func NewBackend(spec ProcessSpec, cmd *analyzerhost.Command, dec Decoder) *Backend
// implements analyzer.ScriptBackend, DescribedScriptBackend, ScriptConfigValidator (schema), ScriptTemplateValidator (version probe; generalizes ruff/version_probe.go)

type Decoder interface { Decode(ctx context.Context, stdout []byte, vars map[string]string) (Result, error) }
type Result struct { Diagnostics []analyzer.ScriptDiagnostic; Sources, Errors []string }
// Envelope{} (process.go:52-56 shape), SARIF{}, JQ{program, limits}

// Proposed additions to internal/plugins/artifacts
type Reference struct{ Scheme, Location, Digest, Platform string }
func (s *Store) Pull(ctx context.Context, ref Reference) (*Artifact, error)             // + "oci" scheme, digest-only
func (s *Store) Materialize(ctx context.Context, digest, filename string) (string, error) // CacheDir/exec/<hex>/<filename>, 0o755, re-hashed per use
```

`tools.Registration`, `tools.Runner`, `tools.Batch/Script/Finding` at `tools.go:34-116` and `execution.Tool` at `internal/checking/scripts/registered_tools.go:14-65` do not change.

Extend `backend.runner.Analyze` at `backend.go:98-212` to:

- Build scripts before checking `run:` steps. At the reviewed revision, only action inputs reach `prepareInput` at `backend.go:120`.
- Map fixes using the code from `shellcheck_fixes.go:30-92`.
- Report findings in files read by the tool, using `shellcheck_sources.go`. Replace the two-value enum at `unavailable_source.go:19-20` with the provider ID in `UnavailableSource`.
- Format diagnostic messages.
- Record `Sources` in `script.Scope.Reader` so file changes trigger another check.

### ShellCheck manifest example

```json
{
  "schema_version": 1,
  "id": "actionlint.kjanat.dev/providers/shellcheck",
  "version": "bundled",
  "role": "script",
  "scope": "source",
  "transport": "process",
  "rule": {
    "name": "shellcheck",
    "description": "Analyze embedded shell scripts with ShellCheck",
    "category": "correctness",
    "subcategory": "shell",
    "maturity": "stable",
    "provider": "shellcheck",
    "recommended": true,
    "documentation_url": "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#shellcheck"
  },
  "preparation": "kjanat:actionlint/shell-preparation@0.3.0",
  "selectors": { "shells": ["bash", "sh", "dash", "ksh"] },
  "capabilities": {
    "fixes": true,
    "reported_sources": true,
    "resources": ["workspace", "directory"],
    "message_template": "shellcheck reported issue in this script: {code}:{severity}:{row}:{column}: {message}"
  },
  "process": {
    "entry": "shellcheck",
    "stdin": "source",
    "exit_codes": [0, 1],
    "args": [
      "{switch:config_file:--rcfile:--norc}",
      "{var:config_file}",
      "{switch:external_sources:-x:}",
      "-f",
      "json1",
      "{flag:dialect:--shell}",
      "-e",
      "{list:exclude:,}",
      "-"
    ],
    "config_files": [".shellcheckrc", "shellcheckrc"],
    "config": { "render": "none" },
    "output": {
      "format": "json",
      "transform": {
        "language": "jq",
        "inline": "{ diagnostics: [ .comments[] | { filename: (if .file == \"-\" then $vars.filename else \"\" end), source: (if .file == \"-\" then \"\" else .file end), code: \"SC\\(.code)\", message: (.message | rtrimstr(\".\")), severity: .level, location: {row: .line, column: .column}, end_location: {row: .endLine, column: .endColumn}, fixes: (if .fix then [{replacements: [ .fix.replacements[] | {row: .line, end_row: .endLine, column: .column, end_column: .endColumn, text: .replacement} ]}] else [] end) } ], sources: .sources }"
      }
    }
  }
}
```

The ShellCheck manifest reproduces the reviewed implementation's command arguments at `analysis_shellcheck_run.go:182-207`, json1 decoding at lines 19-34 and 304-308, `SC%d` codes and messages at lines 264 and 269-270, fixes at `shellcheck_fixes.go:13-23`, and sourced-file findings at lines 244-250.

Actionlint adds the directive prefix and `set -e -o pipefail` startup line when it builds the shell script. It also records the line map and applies the default exclusions `SC1091,...,SC2043` from `input_options.go:12`. These operations use `shell-preparation-0.2.0`, documented at `schemas/shell-preparation-0.2.0.md:15-32`.

A Ruff manifest uses the same structure with these settings:

- Select `preparation: python-preparation@0.1.0`.
- Pass `args: ["check", "{config:toml-pairs:--config}", "--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "--output-format", "json", "--stdin-filename", "{var:filename}", "--extension", "py:python", "{flag:target_version:--target-version}", "-"]`. Command assembly is at `ruff/checker.go:205-214`. The flags at lines 342-360 were cited by an investigator.
- Accept `exit_codes: [0,1]`, as in `adapter.go:68`.
- Probe the version with `version: {args:["--version"], pattern:"^ruff (\\d+\\.\\d+\\.\\d+)", minimum:"0.17.0", template_effects:"0.17.0"}`, from `template_effects.go:20-26`.
- Use the template-effect table `template_effects: {"E501":"line-length", ...}` from `template_effects.go:31-52`.
- Convert flat `target-version/select/ignore` settings to `{target-version, lint:{select,ignore}}`. `backend_process.go:106-126` performs the reverse conversion in the reviewed implementation.
- Format messages with `message_template: "Ruff reported issue in this script: {code}: {message}"`, as in `adapter.go:146`.

## Changes to existing tool integrations

| Existing integration                       | After                                                                                                                                                                                                    | Moves                                                                                                                                                                                                                                                          | Deleted                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Stays generic                                                                                                                                      |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Native process with tool-specific adapters | `transport: process` provider built from a manifest; bundled manifests under `providers/` (embedded) supply the PATH-discovery defaults                                                                  | argv/decoding/version/effect tables → manifest data; python & shell preparation → `internal/plugins/execution/preparation/{python,shell}`; fixes/sources → `backend` runner                                                                                    | `internal/checking/scripts/contract/ruff/{command,native_options,backend_process,compatibility,version_probe,diagnostic,template_effects,adapter,registry,requirements,config,input_options}.go`; `internal/checking/scripts/contract/shellcheck/{analysis_shellcheck*,shellcheck_paths,shellcheck_options,shellcheck_sources,shellcheck_fixes,input_options,registry,adapter,path_context}.go`; `internal/checking/scripts/contract/adapters`; `internal/checking/scripts/contract/selection` (ConfigVariants `requirements.go:87-130` moves to `provider/selection.go`); `backend/process.go` | `analyzerhost.Execution/Command/ProcessPool` (`process.go:19-254`), `processconfig.Options` (`options.go:14-75`), `execution.Tool`, `scriptcoords` |
| Core Wasm JSON ABI                         | `transport: wasm-core`; `core.Metadata` (`engine.go:79-111`) is accepted as a `Manifest` v0 and up-converted (`id`, `engine`, `capabilities.script_shells/inputs`, `template_contract`, `config_schema`) | nothing                                                                                                                                                                                                                                                        | the `descriptor.Engine == registration.Descriptor.Name` matching in `localapp/check.go:109-126` and `input_routing.go:128-130,186-189` becomes "manifest rule name == registration rule name", same code path for all transports                                                                                                                                                                                                                                                                                                                                                                | `core.Engine` untouched; `analyzerhost.ScriptEngines` cache (`script_engines.go:24-47`)                                                            |
| WIT component                              | `transport: wasm-component`; `role: script` → same registration as above; `role: document` → `analyzer.Registration` as today                                                                            | ShellCheck managed glue (`document_plugin.go`, `plugin_sources.go`, `prepared_plugin.go`) → `internal/plugins/manifests/managed`, selects the tool and interface through `Manifest.Integration{Tool, Contract}`; remove the literal at `document_plugin.go:24` | nothing yet (knot 4 sets the retirement)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | `component` loader, roles, sandbox                                                                                                                 |
| Default plugins                            | Default **provider references** in the default config: `plugins: [shellcheck, ruff]` resolved by registry alias as today; bundled manifests are the fallback when nothing is declared                    | `DefaultPluginReferences` (`config_migration.go:30-54`) iterates the registered bundled provider IDs                                                                                                                                                           | the `DefaultPluginOptions{Shellcheck, Ruff bool}` fields (`:21-26`) and the pair at `:39-42`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | `pluginref`, `pluginregistry`, lock/sync                                                                                                           |

### YAML configuration

`tools` becomes a map keyed by the provider's rule name. Existing `ruff` and `shellcheck` keys become entries in that map.

```go
// internal/input/configuration
type ToolsConfig map[string]ToolConfig
type ToolConfig struct {
	Enabled *bool               `yaml:"enabled"`
	Backend *toolconfig.Backend `yaml:"backend,omitempty"` // process | wasm | oci (knot 3)
	Config  *yaml.Node          `yaml:"config,omitempty"`  // engine-owned; validated by the provider's config_schema after registration
}
```

- Bool shorthand such as `tools.ruff: false` works for every tool. See `shellcheck.go:98-105` and `ruff/config.go:56-63`.
- Ruff's flat `target-version/select/ignore` (`internal/input/configuration/tools/ruff/config.go:21-26`) fold into `Config` at decode (unknown sibling keys become `config.<key>`), and the bundled ruff manifest's config transform normalizes them. `internal/input/configuration/tools/ruff` is deleted; remove it from `contracts_test.go:22`.
- `tools.<id>.config` as a scalar string is a path (today ShellCheck's rc path, `config.go:439-445`); `files.go:80,85` resolves scalar paths for every `tools.*.config` setting relative to its configuration file.
- Unknown `tools.<id>` keys are validated against registered providers at the same point `lint.rules.plugins` names are (`lint.go:66-74`; registration-time names already flow through `config.Parse(..., names)`, `docs/plugins.md:543`).
- `lint.rules.external.<id>` = all script providers (bundled or installed); `lint.rules.plugins.<name>` = document analyzers. `rulecatalog.ConfigGroup` (`catalog.go:77-80`) returns `"external"` for any provider rule; `config/catalog.go:63,105` entries go; `Scope` comes from `Manifest.Scope`; `suppression_scope.go:67` is derived from providers with `scope: source`.
- Repo-selected process backends keep requiring `--allow-config-processes` (`selection/process.go:14-19` semantics, moved).

### CLI, environment variables, Go API and GitHub Action

- Flags: `--tool ID[=COMMAND]` (repeatable; bare `--tool ID` = explicit default command, `--tool ID=false` disables) and `--no-tool ID` (repeatable) replace `--ruff`, `--shellcheck`, `--no-ruff`, `--no-shellcheck`, hidden `--pyflakes` (`root.go:241-255`). No alias table; unknown-flag errors point at `--tool/--no-tool`. Explicit `--tool` sets `IgnoreConfig` and `Required` exactly as `env.go:115-120` does today.
- Env: `ACTIONLINT_TOOL_<ID>_BIN|_FLAGS|_ENV` (`<ID>` upper-cased, `-`→`_`); `externalToolEnvironment(prefix)` is already prefix-parameterized (`env.go:201-231`) and loops over registered provider ids.
- Doctor: iterate registrations; for `process` transports resolve the entry and run `Version` from the manifest (`doctor.go:58-59,73-74,120-140` become generic; `probeDoctorRuff` deleted).
- Public API: `analysis.Options.Tools map[string]analysis.Tool` replaces `ShellCheck, Ruff Tool` (`analysis/analysis.go:61-62`); `fileOptions` (`:153-194`) loops the map; `analysis/rules.go:24` registers bundled providers.
- GitHub Action: inputs `shellcheck`, `pyflakes`, `ruff` (`internal/cli/action/inputs.go:103-110`, `lint.go:70-78,101-103`) replaced by `disable-tools` (list); the status line (`outputs.go:101-104`, investigator-cited) lists registered tool ids. Action v2 is unreleased (no v2 tag), so no compatibility shim.
- Playground: `ShellcheckRunner func([]string, string)` (`adapters/registry.go:21`) becomes `transport: embedded` with `provider.Embedded(id, fn)` on top of `analyzerhost.EmbeddedCommand` (`process.go:162-164`).

### Commit 11770d95

Commit `11770d95` maps `--no-ruff` to `"python"` and `--no-shellcheck` to `"shell"` at `cli/plugin.go:72-77`. It then skips plugins whose metadata lists a disabled `languages` entry at `localapp/plugins.go:46-50,195-213`. This happens after artifact resolution at `plugins.go:40`. The review found no other reader or schema for `languages`.

Replace the language mapping, `DisabledLanguages` and `declaresLanguage` with disabling by tool ID. Match `PluginSelection.DisabledTools` against `Pin.Name`, `Pin.Alias` at `model.go:122-123`, and bundled manifest IDs before calling `artifact.Resolve`. Resolve other sources to read their metadata, then disable matching tools before loading them. See the three cases in [the review decisions](tool-providers-decisions.md).

For optional providers, log load failures and skip the provider, following `composition.go:34-37`. This addresses the bug that motivated the commit, where an incompatible artifact aborted the run. The Action's `PluginSelection` also omitted the disabling field at `provisioning.go:32`, according to an investigator. The shared selection data must cover that caller too.

Test that disabling bundled and alias-matched tools happens before resolution with `TestDisabledToolIsNotResolved`. Test that other matching tools are disabled after metadata resolution and before instantiation.

## Design decisions

### K1. Output conversion with jq

The proposal uses `github.com/itchyny/gojq` to convert tool output. It also includes decoders for the native response format and SARIF 2.1.0, which need no jq program. The alternatives considered were CEL, Go `text/template`, a SARIF-only interface and a JSON-pointer field map.

jq was requested for authoring these conversions. The ShellCheck json1 and Ruff examples above each need about ten lines. gojq is pure Go and supports cancellation through a context. Its documented compiler options appear to control environment access, module imports and `input`. This last claim is **inferred** and must be verified before pinning the dependency.

CEL adds protobuf and is oriented towards expressions. `text/template` emits text, so the host would have to parse it as JSON. A SARIF-only interface excludes tools such as ShellCheck that do not emit SARIF. A field map would need extra syntax for operations such as `"SC" + code` and conditionals.

Command arguments use the restricted placeholders described above. They do not run jq. Transform programs are stored by `Transform.Digest` in the artifact cache. Bundled manifests can include them inline.

### K2. Native-process access

This release does not add an OS sandbox for native tools. The alternatives considered were platform-specific sandboxes such as bwrap, seccomp and AppContainer, or allowing only Wasm tools. Wasm execution continues to use its runtime sandbox.

The native-process controls are:

- Verify pinned artifact digests at `resolve.go:102-105` and recheck executable bytes before each use.
- Require `--allow-config-processes` for repository-selected executables. Installing a registry binary requires an explicit `plugin install`.
- Pass literal arguments, as in `command.go:56`.
- Start with a restricted environment and copy only the variables listed in `env_passthrough`. The proposed `Execution.InheritEnvironment` defaults to false for providers. The implementation at `process.go:36-44` inherits `cmd.Environ()`.
- Send the script through stdin and limit stdout size.

These controls govern what actionlint starts and passes to a process. They do not restrict the process's access through OS APIs. The documentation must state this limitation. A portable OS sandbox across the three supported platforms is outside this release.

### K3. OCI artifact downloads and the plugin registry

The proposal uses OCI to distribute artifacts. The existing registry continues to index plugins, aliases, publisher identities and withdrawn releases. Replacing that registry with OCI was considered and rejected.

Add `oci://<host>/<repo>@sha256:<digest>` to `parseSource` at `resolve.go:156-183`. Stored references must use digests. `plugin install` can resolve a tag to a digest, as it resolves `latest` today.

The selected download library is oras-go v3 at `v3.0.0-rc.4`. The maintainer accepted this release candidate during the design review. At that time, `@latest` selected a retracted `v3.0.3-dev` snapshot.

Use `remote.NewRepository` with the repository name. v3 rejects a tag or digest in that argument. Pass the digest to each fetch. Use an anonymous `auth.Client` for `Registry.Client` to answer bearer challenges for public GHCR images. `content.ReadAll` and `content.FetchAll` verify sizes and digests. Set a deadline on every pull because the retry client can honor `Retry-After` for up to 60 seconds.

Only `internal/plugins/artifacts/oci` imports oras-go. A depguard rule enforces this restriction. Its credential helpers link `os/exec`, which must not enter the WebAssembly builds that import `internal/plugins/artifacts`. `artifact.Options.PullOCI` supplies the downloader.

The recorded dependency comparison found `opencontainers/go-digest`, `opencontainers/image-spec` and `x/sync` in oras-go v2.7.0. v3 also adds `BurntSushi/toml`, `yaml.v3`, `ProtonMail/go-crypto`, `cloudflare/circl` and `x/crypto`. Publishing with the `oras` CLI requires no actionlint library support.

An OCI blob digest is the SHA-256 of its bytes. This claim comes from the OCI descriptor specification, which was not reread during the review. Downloaded layers use the existing `CacheDir/<hex>` layout at `resolve.go:251`.

For multiple platforms, publish an OCI index. Each platform manifest contains the provider manifest, executable and transform layers. The plugin declaration pins the index digest, and actionlint selects the entry for `GOOS/GOARCH`.

The proposed lock schema is version 2. Readers accept versions 1 and 2, and writers emit version 2. `Lock.Sources` records the image manifest and layer digests for every platform in a direct `oci://` declaration. The cache plan can then list executable digests before downloading them. Registry `Pin` records do not change until the catalogue supports OCI locations.

The catalogue continues to manage aliases, OIDC identity records and withdrawals through `model.go:57-115`. OCI alone does not provide that publisher verification without a system such as Sigstore.

### K4. Script and document plugins

Use one manifest with separate script and document roles. Extend the script interface with fixes, findings in other files and file-access requests so external linters can use it. The alternatives were to make every plugin a document analyzer or every plugin a script engine.

The Go additions are described above. Stage 7 adds the wire format through `script-engine@0.3.0` and core `check 3`. The host accepts 0.2.0 and 0.3.0. At the reviewed revision, `script-engine.wit:67` excludes fixes, and `docs/design/analysis-contracts.md:167-170` lists the missing fixes, source and resource capabilities.

The `managed-plugin` world continues to work through `provider/managed` and `Manifest.Integration`. It combines `tool-integration`, `prepared-scripts` and `source-files`. Mark it deprecated and retire it after the ShellCheck component is rebuilt against 0.3.0 in its own repository. Document analyzers such as `pipe-to-shell` and `pinned-actions` are unaffected.

### K5. Configuration and API changes before v2

There was no v2 tag at the reviewed revision. `git describe` returned `v1.17.0`. The proposal introduces the manifest-based interfaces before v2.0.

The following formats and behavior stay compatible:

- YAML `tools.ruff` and `tools.shellcheck` entries, boolean shorthand and flat Ruff settings.
- `lint.rules.external.{ruff,shellcheck}`, documented at `docs/config.md:292-296`.
- The `shellcheck` and `ruff` plugin aliases.
- Lock version 1 files, which are read and rewritten during installation.
- Diagnostic messages, codes and severities. `message_template` reproduces `analysis_shellcheck_run.go:264` and `adapter.go:146`, including the text matched by the `ignore-regex` example at `config.go:61`. Renderer cases at `render/result.go:154`, `sarif.go:135` and `langserver/publication.go:16` continue to apply.

The migration deliberately replaces these development interfaces:

- `--ruff`, `--shellcheck`, their `--no-*` flags and `--pyflakes`.
- `ACTIONLINT_RUFF_*` and `ACTIONLINT_SHELLCHECK_*`.
- The Action inputs `ruff`, `shellcheck` and `pyflakes`.
- `analysis.Options.ShellCheck` and `analysis.Options.Ruff`.
- The Ruff launcher grammar at `ruff/command.go:8-259`. Users specify `backend.process.executable` and `backend.process.args` directly, as the ShellCheck example at `docs/config.md:346-352` already does.

Document the replacements in `docs/migration-v2.md`.

### K6. Release scope

This Go release includes stages 0 to 6 and the host changes in stage 7.

Later work covers retiring the managed-plugin interface, rebuilding the ShellCheck and Ruff components against 0.3.0, OS sandboxing, moving aliases and publisher identities into an OCI-native index, and comparing the Rust host against the same manifests. ADR-0008 places tool-specific code outside the Rust host, so its integration would read the manifest data.

### K7. Workflow expression replacement

Shell scripts and generic script tools share the lexer-based replacement code at `backend.go:227-251`, moved to `profile/mask`. Python uses its tokenizer-aware replacement code.

The ShellCheck implementation at `analysis_shellcheck_run.go:86-119` stops at the first `}}`, including one inside an expression string. Replacing it with the lexer changes that behavior. Stage 3 must include a regression fixture for this case.

### K8. Bundled defaults

The design places bundled manifests under `providers/` at the module root. `embed.go` exposes them through `FS`, and registration reads the directory entries. `config init` continues to install registry aliases.

This layout puts the data beside its public embedding package because `go:embed` cannot read parent directories. The documentation generator at `rulecatalog/check-docs` reads the embedded manifests to discover tools. The generator behavior was cited by an investigator.

### K9. Required and optional tools

Whether a tool is required depends on how it was selected. Do not add a manifest field for this.

A tool selected through a flag, environment variable, API option or repository backend is required. A bundled default is optional, so a missing tool produces a warning and is skipped. This follows `env.go:98-99`, `shellcheck/registry.go:36-39` and `composition.go:34-37`.

## Implementation plan

Each stage must pass `go test ./...` with ShellCheck and Ruff 0.17.0 installed, plus the checks at `ci.yml:133-167` and `make lint`, including `actionlintarch`. These commands describe the reviewed revision.

### Stage 0. Add manifests, output decoders and bundled data

Files: new `internal/plugins/manifests/{manifest.go,validate.go,manifest_schema.json}`, `internal/plugins/execution/process/decoder_jq.go` (gojq, limits: no env, no modules, no inputs, context deadline, output cap), `decoder_envelope.go`, `decoder_sarif.go`, `providers/{embed.go, shellcheck/manifest.json, shellcheck/output.jq, ruff/manifest.json, ruff/output.jq, ruff/config.jq}`; `go.mod` adds gojq.
Tests: every bundled manifest validates against the schema; golden decode tests `providers/<id>/testdata/{stdout.json → diagnostics.json}` captured from real `shellcheck -f json1` / `ruff --output-format json` output; jq sandbox tests (env/import/input refused, timeout, cap); SARIF fixture.
CI changes in this stage must be additive.

### Stage 1. Read tool settings from manifests

Files: `internal/input/configuration/config.go:418-446` (map `ToolsConfig`/`ToolConfig`), `shellcheck.go:76-81,98-115`, `files.go:80,85`, `tools/command.go:110-120`, `catalog.go:63,105`, `suppression_scope.go:67`, delete `internal/input/configuration/tools/ruff`, `internal/checking/registry/index.go:22,55,78`, `internal/localapp/plugin_cache.go:40-43`, `internal/checking/scripts/contract/selection/{module,process,requirements}.go` → `cfg.Tools[name]` (temporary), `internal/architecture/contracts_test.go:22`, `internal/development/generators/config-schema/main.go` schema generator.
Tests: existing config parse/override tests unchanged in expectation; new: bool shorthand through the map, flat Ruff keys fold into `config`, unknown `tools.<x>` rejected without a registered provider and accepted with one, inherited `tools.*.config` path relativization, JSON-schema output diff.

### Stage 2. Run tools and convert their findings

Files: `analyzer/backend.go` (+`Source`, `Fixes`), `internal/plugins/execution/process/{backend.go,args.go,config_render.go,version.go}`, `internal/integrations/process/process.go` (+`InheritEnvironment`), `internal/plugins/execution/scripts/backend.go` (script-building call for run scripts at `:118-130`, fixes, sources, message template), new `internal/plugins/execution/scripts/{fixes.go,sources.go}` ported from `shellcheck_fixes.go:30-92` and `shellcheck_sources.go`, delete `internal/plugins/execution/scripts/process.go`, update `internal/checking/tools_registry_test.go:24,38` to the new transport.
Tests: args placeholder table; `toml-pairs` rendering equals today's `native_options.go` output (port `native_options_test.go`); version probe (min/exact); env scrubbing + passthrough; generic fix mapping (port `shellcheck_fixes_test.go`); reported-source finding provenance; process integration via the test-binary helper pattern (`tools_registry_test.go:53-72`, `config_tool_command_test.go:20-51`) for envelope, jq and SARIF decoders.

### Stage 3. Build scripts for linters

Files: new `internal/plugins/execution/preparation/{profile.go, mask/, shell/, python/}`; move `shellcheck_shell.go`, `shellcheck_script.go:44-113`, `prepared_plugin.go`, `path_context.go` → `profile/shell`; `ruff/checker.go` sanitize/`templateMasks`/python-shell/target inference → `profile/python`; `backend.go:227-251` → `profile/mask`. `ruff` and `shellcheck` import the moved script-processing code. Behavior is unchanged except for the expression-replacement fix in K7.
Tests: move `shellcheck_shell_test.go`, `shellcheck_script_test.go`, `prepared_plugin_test.go`, `ruff/sanitize_regression_test.go` and the `*_review_test.go` sanitizer suites unchanged; new K7 fixture (`${{ '}}' }}` inside a string).

### Stage 4. Register tools from manifests and remove their adapters

Split the Ruff and ShellCheck migrations if they are too large to review together.
Files: new `internal/plugins/manifests/{registration.go,selection.go,bundled.go,managed/}`; `analysis/analysis.go:141-196`, `analysis/rules.go:24`, `internal/cli/action/lint.go:127-132`, Playground wiring → `provider.Bundled()` + `Tools map`; `internal/results/finding/unavailable_source.go:16-52` producer → id; delete `internal/checking/scripts/contract/adapters`, `internal/checking/scripts/contract/selection`, `internal/checking/scripts/contract/ruff/*`, `internal/checking/scripts/contract/shellcheck/*` (managed glue moves to `provider/managed`, generic over `Manifest.Integration`).
Tests: Before deleting adapters, capture snapshots from the current adapters (the `capture_fixture_test.go` pattern already exists in `ruff/`, `shellcheck/`, `backend/`) and assert identical findings (message, code, severity, start/end, fixes, foreign sources, suppression origin) through the manifest route; re-point `internal/development/tests/integration/tools/*` (`composite_ruff_test.go`, `ruff_process_test.go`, `shellcheck_{fixes,paths,runner,shell,sources}_test.go`, `config_tool_command_test.go:63-77`) from `adapters.Options{...}` to `provider.Bundled()`; `internal/development/tests/integration/cli/plugin_test.go:88-161` (`ACTIONLINT_TEST_{RUFF,SHELLCHECK}_WASM`) unchanged.

### Stage 5. Update commands, APIs and plugin disabling

Files: `internal/cli/root.go:236-256`, `env.go:83-125`, `plugin.go:68-95`, `doctor.go:54-84,120-140`, `help.go:249-251`, `check.go:49`; `analysis/analysis.go:40-67`; `internal/cli/action/{inputs.go:103-126, lint.go:67-106,155-157, main.go:10-23, outputs.go}` + `action.yml`; `internal/localapp/{config_migration.go:19-54,121-125, plugins.go:31-82,195-213, plugin_selection.go:21-46,106-113}`; delete `internal/localapp/plugin_languages_test.go`; docs `config.md`, `usage.md`, `env.md`, `action.md`, `migration-v2.md`, `plugins.md`.
Tests: CLI `--tool/--no-tool` parsing incl. `=false`, env prefix per id, doctor JSON snapshot with a manifest probe, Action input tests, `TestDisabledToolIsNotResolved` (no `artifact.Resolve` call for a disabled bundled or alias-matched tool), metadata resolution followed by disabling for other sources, public API example tests.

### Stage 6. Download OCI artifacts and write lock version 2

Files: `internal/plugins/artifacts/{resolve.go (oci scheme), oci.go, inspect.go (Kind binary/manifest; Sniff only for wasm kinds), store.go (Materialize)}`, `internal/plugins/installation/{model.go, lock.go}`, `internal/localapp/{plugins.go (dispatch on Manifest.Transport), plugin_cache.go}`, `internal/cli/plugin.go` (install/inspect OCI refs), `plugin/plugin.go` aliases, docs `plugins.md`.
Tests: `httptest` OCI distribution fixture (index → platform manifest → blobs, bearer challenge), digest mismatch, tag reference refused in lock, platform selection, Windows filename materialization (`.exe`; `os/exec` extension handling is **inferred**), `plugin sync` offline from cache, `CachePlan` lists binary digests, lock v1 → v2 round trip.

### Stage 7. Update the host for script-engine 0.3.0

Files: `plugins/protocol/wit/script-engine.wit` → `@0.3.0` (+`fixes`, `source`, `resources` negotiate), `core-json-abi.md` check 3, `internal/plugins/execution/component/{script_engine.go,validation.go:34-65,contract.gen.go (regen)}`, `internal/plugins/execution/module/engine.go` negotiation, SDK crate; 0.2.0 still accepted.
Tests: 0.3.0 fixture components under `internal/plugins/execution/component/testdata` (Rust + wat), negotiation and version-mismatch tests, parity between the process route and a 0.3.0 component on the same stdout fixture.

Stages 0 to 3 add code or move existing code. Stage 4 replaces the adapters and compares their findings against captured results. Stage 5 changes commands and APIs after manifest-based execution works. Stage 6 adds artifact distribution. Stage 7 updates guest interfaces last, so guest rebuilds do not block the earlier work.
