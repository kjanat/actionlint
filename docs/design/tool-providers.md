# Tool providers: one layer above transports

Design decision for actionlint v2 (Go). Read-only review of `C:\Users\kjanat\Projects\actionlint\.worktrees\go-v2` at `f8d5a6c3` (branch `go-identity-model`). Every claim cites `file:line` I read; items I did not read in this session are marked **inferred** or **investigator-cited**.

## 0. Ground truth that shapes the decision

- The host already consumes tools only through capability closures: `tools.Registration{Enabled, Required, Build, NeedsProcess, WithBackend, WithAnalyzer, ConfiguredBackend, PrepareInput, InputSelectors}` (`internal/tools/tools.go:34-52`); composition calls only `Enabled/Required/Build` (`internal/application/composition.go:28-44`). The package doc promises independence from "tool names and transports" (`tools.go:1`).
- The transport-neutral snippet contract exists and has three implementers: `analyzer.ScriptBackend.Check(ctx, ScriptRequest) ([]ScriptDiagnostic, error)` (`analyzer/backend.go:16-18,117-121,139-149`); `core.Engine`, `component.Plugin`, `ruff.ProcessBackend` (`internal/tools/ruff/backend_process.go:88`). ShellCheck's native path is **not** a `ScriptBackend`; it converts backend diagnostics back into its private json1 struct (`internal/tools/shellcheck/analysis_shellcheck_run.go:288-297`).
- A fully generic `ScriptDiagnostic -> tools.Finding` runner exists: `backend.runner.Analyze` (`internal/tools/backend/backend.go:98-212`): shell/input selection, template-effect gate, filename check, `AcceptDiagnostic`, `OriginalLine`, `Position/EndPos` mapping, `Key` fallback. The same fallback is copied twice more (`internal/tools/ruff/adapter.go:141-145`, `analysis_shellcheck_run.go:261-267`).
- A generic native-process transport exists and has **no production caller**: `backend.Process` + `processBackend.Check` with the `{ok,diagnostics,errors}` envelope (`internal/tools/backend/process.go:19-71`); only `internal/application/tools_registry_test.go:24,38` use it.
- The content-addressed store exists: `CacheDir/<sha256hex>`, re-hashed on read, atomic publish (`internal/plugins/artifact/resolve.go:93,251-257,312-335`); `Import(reader, digest)` adds verified bytes (`resolve.go:188-208`); `sha256://` is a cache-only scheme (`resolve.go:157-162`). Blockers for binaries: scheme allow-list `file|http|https|sha256` (`resolve.go:156-183`) and `Sniff` rejecting non-wasm (`inspect.go:48-49`).
- Pins and locks are already tool-neutral: `Pin{URL, SHA256, Kind, Contract, Alias, PluginID}` (`internal/pluginregistry/model.go:119-141`), `Lock{schema_version:1, plugins}` (`lock.go:24-27`).
- Tool names leak in exactly the places the investigators listed; I verified the structural ones: fixed `ToolsConfig{Ruff, Shellcheck}` (`internal/config/config.go:419-424`); `if name == "ruff"` switches (`internal/tools/selection/module.go:22-25`, `process.go:27-30`, `requirements.go:69-72,149-152`); fixed `adapters.Options` fields (`internal/tools/adapters/registry.go:16-34`); `NormalizePaths` literal list (`internal/config/tools/command.go:110`); `normalizeToolSwitch` literal list (`internal/config/shellcheck.go:76`); catalog entries (`internal/config/catalog.go:63,105`; `internal/rulecatalog/catalog.go:22,55,78`); CLI flags (`internal/cli/root.go:241-255`, `env.go:87-120`); plugin language table (`internal/cli/plugin.go:72-77`); default plugin injection (`internal/localapp/config_migration.go:39-42`); cache plan (`internal/localapp/plugin_cache.go:40-43`); public API map (`analysis/analysis.go:61-62,162,189-193`); provenance enum (`internal/finding/unavailable_source.go:19-20,36-48`); managed contract literal (`internal/tools/shellcheck/document_plugin.go:24`). `rg` counts 46 further literal `"ruff"|"shellcheck"` lines in production Go outside the two tool packages and `internal/config`.
- No transform library is present: `go.mod:7-32` has no jq/CEL/JSONPath dependency; `go.sum` has none either. Deps that matter: `go-toml/v2` (`go.mod:19`), `santhosh-tekuri/jsonschema/v6` (`go.mod:21`), `encoding/json/v2` via Go 1.27 (`docs/design/json-processing.md:3-5`).
- **No v2 tag exists**: `git tag --list 'v2*'` is empty; `git describe` gives `v1.17.0`. The "v2 configs in the wild" are pre-release/nightly users and the docs (`docs/config.md:283-296,345-361`).
- Architecture guards that constrain where new code may live: `internal/config`, `internal/plugins/{core,component}`, `plugin` must not transitively import `internal/engine`, `internal/cli`, `os/exec`, `analysis`, `config` (`internal/architecture/contracts_test.go:12-52`); `internal/engine`, `internal/application`, `analysis` must not import `internal/plugins/component` or wazero/wacogo (`contracts_test.go:140-151`). So manifest/process code goes under `internal/tools/...`; Wasm instantiation stays in `localapp` behind `tools.Context.LoadEngine` (`tools.go:24`), as `docs/design/application-boundary.md:31-35` already requires.
- The owner's `rust-adr` worktree already decided the end state for the Rust host: no tool names in-repo, scripts exposed as providers (`.worktrees/identity-model/docs/adr/0008-no-tool-specific-code.md:15-19`); extension distribution out of scope there (`0009-extension-contract.md:125`). This Go design is the bridge: tools become data the Rust host can consume unchanged.

## 1. The abstraction: **Tool Providers**

A *tool provider* is data: a manifest (content-addressed) that names a rule, a transport, the artifacts to fetch by digest, how to invoke them, how to turn their output into the diagnostic record, and which host-owned *preparation profile* feeds them. The host has one registration constructor that turns a manifest into `tools.Registration`. Nothing in the host names a tool.

### Layers

| Layer | Owns | Today's code that becomes it |
| --- | --- | --- |
| L0 **Store** (CAFS) | bytes by sha256; sources `path`, `file`, `https#sha256`, `sha256://`, **`oci://…@sha256:`**; materialization of executables | `internal/plugins/artifact/resolve.go:68-127,188-208,248-335`, `inspect.go:47-96` |
| L1 **Manifest** | identity, role, rule descriptor, transport, per-platform artifact digests, selectors, capabilities, invocation template, output format + transform, config rendering/schema, version probe, template-effect table | core `Metadata` (`internal/plugins/core/engine.go:79-111`), component static metadata (`internal/plugins/component/script_engine.go:404-464`), `actionlint.plugin` section (`inspect.go:16`), plus everything currently hard-coded in `ruff/{command,native_options,compatibility,template_effects,diagnostic}.go` and `shellcheck/{analysis_shellcheck_run,input_options,shellcheck_paths}.go` |
| L2 **Transport** → `analyzer.ScriptBackend` | `process` (new, generic), `wasm-core` (`core.Engine`), `wasm-component` (`component.Plugin`), `embedded` (Go callers; Playground) | `backend/process.go:41-71`, `ruff/backend_process.go:88-172`, `shellcheck` `externalCommand` (`analysis_shellcheck_run.go:300-310`), `analyzerhost.EmbeddedCommand` (`internal/analyzerhost/process.go:162-164`) |
| L3 **Preparation profile** (language, not tool) | masking, dialect/target inference, injected directive/startup lines + line map, template-mask provenance filter, argv variables, resource requests | shell: `shellcheck_shell.go`, `shellcheck_script.go:44-113`, `prepared_plugin.go` (= `schemas/shell-preparation-0.2.0.md:1-35`); python: `ruff/checker.go` sanitize/`templateMasks`/python-shell detection (investigator: 226-340, 369-431, 490-642); generic mask: `backend/backend.go:227-251` |
| L4 **Runner** | `ScriptDiagnostic -> tools.Finding`: validation, line remap, YAML mapping, fix mapping, reported-source findings, message template, severity passthrough | `backend/backend.go:98-212` + `shellcheck_fixes.go:30-92` + `shellcheck_sources.go` + the two duplicated fallbacks |
| L5 **Registration/composition** | `provider.Register(manifest, backend, profile, selection) -> tools.Registration`; precedence explicit > repo backend > locked plugin > bundled default | `adapters/registry.go`, `selection/*`, `ruff/registry.go`, `shellcheck/registry.go`, `localapp/check.go:99-144`, `application/tool_integrations.go:18-83` |
| L6 **Policy** | unchanged, keyed by rule name only (`internal/suppression/inline.go:122`; `diagnosticpolicy` investigator-cited) | unchanged |

### Manifest (JSON; `schema_version: 1`)

```go
// internal/tools/provider/manifest.go — data only; imports analyzer, rule, encoding/json
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
	Preparation   string          `json:"preparation"`     // profile id, see L3
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
	Digest   string `json:"digest,omitempty"` // content-addressed program (the owner's "oid")
	Inline   string `json:"inline,omitempty"` // bundled manifests only
}
type VersionProbe struct {
	Args            []string `json:"args"`
	Pattern         string   `json:"pattern"`                    // RE2, one capture group
	Minimum         string   `json:"minimum,omitempty"`
	TemplateEffects string   `json:"template_effects,omitempty"` // exact version the TemplateEffects table was reviewed against
}
```

Argv placeholder grammar (closed; no evaluation): `{var:NAME}`, `{flag:NAME:--opt}` (emits `--opt value` when the profile set NAME), `{switch:NAME:--on:--off}`, `{list:NAME:SEP}`, `{config:toml-pairs:--config}` (repeated `--config "k" = <inline TOML>` exactly as `native_options.go:19-44` does today, investigator-cited), `{config:toml-file}`/`{config:json-file}` (temp file path). Variables come from the preparation profile (`dialect`, `target_version`, `exclude`, `filename`, `config_file`, `external_sources`).

Transform contract: input `.` is parsed stdout (`json`) or the raw string (`text`); `$vars` is the same variable map plus `exit_code`; output is `{"diagnostics":[...], "sources":[...], "errors":[...]}` or a bare diagnostics array; each diagnostic is `analyzer.ScriptDiagnostic` extended with `source` and `fixes` (below). The host validates the output against a JSON schema (`jsonschema/v6`, `go.mod:21`) and applies the same caps the component host applies (investigator: `script_engine.go:134-229`: 8 MiB, 10000 diagnostics).

### Go contract growth (host side, JSON-compatible superset)

```go
// analyzer/backend.go — additive
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
// internal/tools/provider — runtime-free (no wasm imports; passes contracts_test.go:140-151)
func Parse(data []byte) (Manifest, error)                 // schema-validated, jsonv2 (duplicate keys rejected)
func Register(m Manifest, backend analyzer.ScriptBackend, profile Profile, sel Selection) (tools.Registration, error)

type Selection struct {              // replaces internal/tools/selection and the Optional/IgnoreConfig dance in processconfig.Options:15-22
	Explicit *processconfig.Options // CLI/API/env; nil = manifest defaults
	Disabled bool
	Required bool                    // explicit selection => Build errors fatal (composition.go:34-37 semantics kept)
}

// internal/tools/profile — preparation profiles
type Profile interface {
	ID() string                                            // "kjanat:actionlint/shell-preparation@0.2.0" | ".../python-preparation@0.1.0" | ".../expression-mask@0.1.0"
	Prepare(script tools.Script, config json.RawMessage) (Prepared, error)
	Preparer() analyzer.ScriptPreparer                     // analyzer/tool_integration.go:79; nil when the profile has none
}
type Prepared struct {
	tools.PreparedInput                   // tools.go:56-63 unchanged
	Vars      map[string]string           // argv variables
	Resources []analyzer.ToolResourceRequest
}

// internal/tools/provider/process — the one native transport
type Backend struct{ /* spec, *analyzerhost.Command, Decoder, limits, probe cache */ }
func NewBackend(spec ProcessSpec, cmd *analyzerhost.Command, dec Decoder) *Backend
// implements analyzer.ScriptBackend, DescribedScriptBackend, ScriptConfigValidator (schema), ScriptTemplateValidator (version probe; generalizes ruff/version_probe.go)

type Decoder interface { Decode(ctx context.Context, stdout []byte, vars map[string]string) (Result, error) }
type Result struct { Diagnostics []analyzer.ScriptDiagnostic; Sources, Errors []string }
// Envelope{} (process.go:52-56 shape), SARIF{}, JQ{program, limits}

// internal/plugins/artifact — store growth
type Reference struct{ Scheme, Location, Digest, Platform string }
func (s *Store) Pull(ctx context.Context, ref Reference) (*Artifact, error)             // + "oci" scheme, digest-only
func (s *Store) Materialize(ctx context.Context, digest, filename string) (string, error) // CacheDir/exec/<hex>/<filename>, 0o755, re-hashed per use
```

`tools.Registration`, `tools.Runner`, `tools.Batch/Script/Finding` (`tools.go:34-116`) and `execution.Tool` (`internal/tools/execution/registered_tools.go:14-65`) are unchanged. `backend.runner.Analyze` (`backend.go:98-212`) absorbs: profile preparation for run scripts (today only action inputs reach `prepareInput`, `backend.go:120`), fix mapping (from `shellcheck_fixes.go:30-92`), reported-source findings (from `shellcheck_sources.go`, with the `UnavailableSource` producer becoming the provider id instead of the two-value enum at `unavailable_source.go:19-20`), message templating, and `Sources` recording into `script.Scope.Reader` for watch invalidation.

### A bundled manifest, to prove "tools are data" (ShellCheck)

```json
{
  "schema_version": 1, "id": "actionlint.kjanat.dev/providers/shellcheck", "version": "bundled",
  "role": "script", "scope": "source", "transport": "process",
  "rule": { "name": "shellcheck", "description": "Analyze embedded shell scripts with ShellCheck", "category": "correctness",
            "subcategory": "shell", "maturity": "stable", "provider": "shellcheck", "recommended": true, "fix_availability": "suggested",
            "documentation_url": "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#shellcheck" },
  "preparation": "kjanat:actionlint/shell-preparation@0.2.0",
  "selectors": { "shells": ["bash", "sh", "dash", "ksh"] },
  "capabilities": { "fixes": true, "reported_sources": true, "resources": ["workspace", "directory"],
                    "message_template": "shellcheck reported issue in this script: {code}:{severity}:{row}:{column}: {message}" },
  "process": {
    "entry": "shellcheck", "stdin": "source", "exit_codes": [0, 1],
    "args": ["{switch:config_file:--rcfile:--norc}", "{var:config_file}", "{switch:external_sources:-x:}",
             "-f", "json1", "{flag:dialect:--shell}", "-e", "{list:exclude:,}", "-"],
    "config_files": [".shellcheckrc", "shellcheckrc"],
    "config": { "render": "none" },
    "output": { "format": "json", "transform": { "language": "jq", "inline":
      "{ diagnostics: [ .comments[] | { filename: (if .file == \"-\" then $vars.filename else \"\" end), source: (if .file == \"-\" then \"\" else .file end), code: \"SC\\(.code)\", message: (.message | rtrimstr(\".\")), severity: .level, location: {row: .line, column: .column}, end_location: {row: .endLine, column: .endColumn}, fixes: (if .fix then [{replacements: [ .fix.replacements[] | {row: .line, end_row: .endLine, column: .column, end_column: .endColumn, text: .replacement} ]}] else [] end) } ], sources: .sources }" } }
  }
}
```

This reproduces today's argv (`analysis_shellcheck_run.go:182-207`), json1 decoding (`:19-34,304-308`), the `SC%d` code and message text (`:264,269-270`), fix replacements (`shellcheck_fixes.go:13-23`) and sourced-file findings (`:244-250`). The directive prefix, `set -e -o pipefail` startup line, line map and default exclusions `SC1091,…,SC2043` (`input_options.go:12`) are the shell profile, i.e. `shell-preparation-0.2.0` (`schemas/shell-preparation-0.2.0.md:15-32`), not the manifest. Ruff's manifest is the same shape with `preparation: python-preparation@0.1.0`, `args: ["check", "{config:toml-pairs:--config}", "--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "--output-format", "json", "--stdin-filename", "{var:filename}", "--extension", "py:python", "{flag:target_version:--target-version}", "-"]` (assembly today at `ruff/checker.go:205-214`, flags investigator-cited `:342-360`), `exit_codes: [0,1]` (`adapter.go:68`), `version: {args:["--version"], pattern:"^ruff (\\d+\\.\\d+\\.\\d+)", minimum:"0.17.0", template_effects:"0.17.0"}` (`template_effects.go:20-26`), `template_effects: {"E501":"line-length", …}` (`template_effects.go:31-52`), a config transform folding flat `target-version/select/ignore` into `{target-version, lint:{select,ignore}}` (what `backend_process.go:106-126` unfolds today), and `message_template: "Ruff reported issue in this script: {code}: {message}"` (`adapter.go:146`).

## 2. How the four routes collapse

| Route today | After | Moves | Deleted | Stays generic |
| --- | --- | --- | --- | --- |
| **Native process** (bespoke adapters) | `transport: process` provider built from a manifest; bundled manifests under `providers/` (embedded) supply the PATH-discovery defaults | argv/decoding/version/effect tables → manifest data; python & shell preparation → `internal/tools/profile/{python,shell}`; fixes/sources → `backend` runner | `internal/tools/ruff/{command,native_options,backend_process,compatibility,version_probe,diagnostic,template_effects,adapter,registry,requirements,config,input_options}.go`; `internal/tools/shellcheck/{analysis_shellcheck*,shellcheck_paths,shellcheck_options,shellcheck_sources,shellcheck_fixes,input_options,registry,adapter,path_context}.go`; `internal/tools/adapters`; `internal/tools/selection` (ConfigVariants `requirements.go:87-130` moves to `provider/selection.go`); `backend/process.go` | `analyzerhost.Execution/Command/ProcessPool` (`process.go:19-254`), `processconfig.Options` (`options.go:14-75`), `execution.Tool`, `scriptcoords` |
| **Core-wasm JSON ABI** | `transport: wasm-core`; `core.Metadata` (`engine.go:79-111`) is accepted as a `Manifest` v0 and up-converted (`id`, `engine`, `capabilities.script_shells/inputs`, `template_contract`, `config_schema`) | nothing | the `descriptor.Engine == registration.Descriptor.Name` matching in `localapp/check.go:109-126` and `input_routing.go:128-130,186-189` becomes "manifest rule name == registration rule name", same code path for all transports | `core.Engine` untouched; `analyzerhost.ScriptEngines` cache (`script_engines.go:24-47`) |
| **WIT component** | `transport: wasm-component`; `role: script` → same registration as above; `role: document` → `analyzer.Registration` as today | ShellCheck managed glue (`document_plugin.go`, `plugin_sources.go`, `prepared_plugin.go`) → `internal/tools/provider/managed`, generic over `Manifest.Integration{Tool, Contract}` instead of the literal at `document_plugin.go:24` | nothing yet (knot 4 sets the retirement) | `component` loader, roles, sandbox |
| **Auto-added default plugins** | Default **provider references** in the default config: `plugins: [shellcheck, ruff]` resolved by registry alias as today; bundled manifests are the fallback when nothing is declared | `DefaultPluginReferences` (`config_migration.go:30-54`) iterates registered bundled provider ids instead of a literal pair | the `DefaultPluginOptions{Shellcheck, Ruff bool}` fields (`:21-26`) and the pair at `:39-42` | `pluginref`, `pluginregistry`, lock/sync |

### Config surface (YAML)

`tools` becomes a map keyed by provider rule name; the keys `ruff`/`shellcheck` keep parsing because they are now data, not fields:

```go
// internal/config
type ToolsConfig map[string]ToolConfig
type ToolConfig struct {
	Enabled *bool               `yaml:"enabled"`
	Backend *toolconfig.Backend `yaml:"backend,omitempty"` // process | wasm | oci (knot 3)
	Config  *yaml.Node          `yaml:"config,omitempty"`  // engine-owned; validated by the provider's config_schema after registration
}
```

- Bool shorthand (`tools.ruff: false`, `shellcheck.go:98-105`; `ruff/config.go:56-63`) stays, generic.
- Ruff's flat `target-version/select/ignore` (`internal/config/tools/ruff/config.go:21-26`) fold into `Config` at decode (unknown sibling keys become `config.<key>`), and the bundled ruff manifest's config transform normalizes them. `internal/config/tools/ruff` is deleted; remove it from `contracts_test.go:22`.
- `tools.<id>.config` as a scalar string is a path (today ShellCheck's rc path, `config.go:439-445`); `files.go:80,85` relativizes any `tools.*.config` scalar, not `tools.shellcheck.config`.
- Unknown `tools.<id>` keys are validated against registered providers at the same point `lint.rules.plugins` names are (`lint.go:66-74`; registration-time names already flow through `config.Parse(..., names)`, `docs/plugins.md:543`).
- `lint.rules.external.<id>` = all script providers (bundled or installed); `lint.rules.plugins.<name>` = document analyzers. `rulecatalog.ConfigGroup` (`catalog.go:77-80`) returns `"external"` for any provider rule; `config/catalog.go:63,105` entries go; `Scope` comes from `Manifest.Scope`; `suppression_scope.go:67` is derived from providers with `scope: source`.
- Repo-selected process backends keep requiring `--allow-config-processes` (`selection/process.go:14-19` semantics, moved).

### CLI / env / API / Action

- Flags: `--tool ID[=COMMAND]` (repeatable; bare `--tool ID` = explicit default command, `--tool ID=false` disables) and `--no-tool ID` (repeatable) replace `--ruff`, `--shellcheck`, `--no-ruff`, `--no-shellcheck`, hidden `--pyflakes` (`root.go:241-255`). No alias table; unknown-flag errors point at `--tool/--no-tool`. Explicit `--tool` sets `IgnoreConfig` and `Required` exactly as `env.go:115-120` does today.
- Env: `ACTIONLINT_TOOL_<ID>_BIN|_FLAGS|_ENV` (`<ID>` upper-cased, `-`→`_`); `externalToolEnvironment(prefix)` is already prefix-parameterized (`env.go:201-231`) and loops over registered provider ids.
- Doctor: iterate registrations; for `process` transports resolve the entry and run `Version` from the manifest (`doctor.go:58-59,73-74,120-140` become generic; `probeDoctorRuff` deleted).
- Public API: `analysis.Options.Tools map[string]analysis.Tool` replaces `ShellCheck, Ruff Tool` (`analysis/analysis.go:61-62`); `fileOptions` (`:153-194`) loops the map; `analysis/rules.go:24` registers bundled providers.
- GitHub Action: inputs `shellcheck`, `pyflakes`, `ruff` (`internal/githubaction/inputs.go:103-110`, `lint.go:70-78,101-103`) replaced by `disable-tools` (list); the status line (`outputs.go:101-104`, investigator-cited) lists registered tool ids. Action v2 is unreleased (no v2 tag), so no compatibility shim.
- Playground: `ShellcheckRunner func([]string, string)` (`adapters/registry.go:21`) becomes `transport: embedded` with `provider.Embedded(id, fn)` on top of `analyzerhost.EmbeddedCommand` (`process.go:162-164`).

### Commit 11770d95

It maps `--no-ruff → "python"` and `--no-shellcheck → "shell"` (`cli/plugin.go:72-77`) and skips plugins whose untrusted metadata lists a disabled `languages` entry (`localapp/plugins.go:46-50,195-213`), after the artifact has already been resolved (`plugins.go:40` precedes `:46`). The `languages` key is read nowhere else (`rg` confirms only `plugins.go:202`) and has no schema. **Disposition: revert the language mapping, `DisabledLanguages`, and `declaresLanguage`; replace with id-based disabling evaluated before resolution.** `PluginSelection.DisabledTools []string` is matched against `Pin.Name`/`Pin.Alias` (`model.go:122-123`) and bundled manifest ids *before* `artifact.Resolve`, so a disabled tool is never downloaded or loaded. Its motivating bug ("incompatible artifact aborts the run") is additionally fixed by treating a load failure of a non-required provider as a logged skip, mirroring `composition.go:34-37`. The Action gap (its `PluginSelection` omits the field, `provisioning.go:32`, investigator-cited) closes because disabling is now plain selection data. Keep the test intent as `TestDisabledToolIsNotResolved`.

## 3. Knots

**K1. Declarative transform language.** Options: jq (gojq), CEL, Go `text/template`, SARIF-only, JSON-pointer field map. **Decision: jq via `github.com/itchyny/gojq`, plus two built-in decoders that need no program: the native envelope (`process.go:52-56`) and SARIF 2.1.0.** Why: it is what the owner asked for; ShellCheck json1 and Ruff JSON reshape in ~10 lines each (above); gojq is pure Go, context-cancellable, and keeps environment access, module imports and `input` opt-in via compiler options (**inferred** from gojq's documented API; verify before pinning). CEL is boolean-expression shaped and drags protobuf; `text/template` emits text, not JSON; SARIF-only excludes every tool without a SARIF emitter (ShellCheck); a field map cannot express `"SC"+code` or conditionals without growing into a language. Argv is **not** jq: argv uses the closed placeholder grammar so repository config can never evaluate into arbitrary flags. Transform programs are content-addressed blobs (`Transform.Digest`) resolved through the same store; bundled manifests inline them.

**K2. Are binary providers sandboxed?** Options: OS sandbox (bwrap/seccomp/AppContainer), Wasm-only, policy sandbox. **Decision: policy sandbox, no OS sandbox this release; Wasm stays the sandboxed route and the docs say so.** Concretely: digest-pinned bytes (`resolve.go:102-105`), explicit opt-in for repository-selected processes (`--allow-config-processes`), registry-installed binaries only via an explicit `plugin install`, literal argv (`command.go:56`), scrubbed child environment (new `Execution.InheritEnvironment bool`, default false for providers; today `cmd.Environ()` is inherited at `process.go:36-44`) plus `env_passthrough`, stdin-only input, bounded stdout, materialized executables re-hashed per use. OS sandboxes are not portable across the three supported platforms and would be a second, half-true security story.

**K3. OCI/ORAS vs the existing registry.** Options: replace the registry with OCI; add OCI as a source scheme; keep the registry as index and OCI as transport. **Decision: OCI is the distribution substrate, the registry stays the index.** Add `oci://<host>/<repo>@sha256:<digest>` to `parseSource` (`resolve.go:156-183`), digest references only (tags resolve to digests at `plugin install`, like `latest` today); a minimal pull-by-digest client (manifest/index GET + blob GET + anonymous bearer challenge, **inferred** requirement for GHCR) in `internal/plugins/artifact/oci.go`, no `oras-go` dependency (it pulls the docker/opencontainers module graph; publishing with the `oras` CLI needs nothing from us). An OCI blob digest is the sha256 of the blob bytes (OCI descriptor spec, not re-read here), so pulled layers land at today's `CacheDir/<hex>` (`resolve.go:251`). Multi-platform binaries are an OCI index whose per-platform manifests carry the provider manifest layer, the binary layer and the transform layer; the lock pins the index digest and the host selects `GOOS/GOARCH`. Lock schema bumps to 2 (`lock.go:30` reads 1 and 2; `Pin` gains `Platforms map[string]Artifact` and `Manifest` digest). The catalogue keeps aliases, OIDC identity receipts and withdrawals (`model.go:57-115`), none of which OCI provides without sigstore.

**K4. Unify analyzer vs script-engine contracts.** Options: everything a document analyzer; everything a script engine; one manifest with two roles and a script contract rich enough that no *tool* needs the analyzer role. **Decision: the third.** The snippet contract gains `fixes`, `source` (reported foreign files) and resource negotiation; on the Go side now (section 1), on the wire as `script-engine@0.3.0` + core `check 3` in stage 7 (host accepts both 0.2.0 and 0.3.0). `script-engine.wit:67` explicitly withholds fixes today, and `docs/design/analysis-contracts.md:167-170` names exactly these three missing capabilities. The `managed-plugin` world (`tool-integration` + `prepared-scripts` + `source-files`) keeps working through `provider/managed`, generic over `Manifest.Integration`, and is marked deprecated; it is retired once the ShellCheck component is rebuilt against 0.3.0 (its repo, not this one). Document analyzers (`pipe-to-shell`, `pinned-actions`) are unaffected.

**K5. Backward compatibility for v2 configs.** **Decision: v2.0 ships the generic surface; there is no v2 tag to be compatible with** (`git describe` → `v1.17.0`). What stays byte-compatible by construction: YAML `tools.ruff`/`tools.shellcheck` keys (map entries), bool shorthand, flat Ruff keys (folded), `lint.rules.external.{ruff,shellcheck}` (`docs/config.md:292-296`), `plugins: [shellcheck, ruff]` aliases, lock v1 files (read; rewritten on install), diagnostic message text (manifest `message_template` reproduces `analysis_shellcheck_run.go:264` and `adapter.go:146`, so the documented `ignore-regex` example at `config.go:61` keeps matching), codes, severities (renderers keep their `style` cases at `render/result.go:154`, `sarif.go:135`, `langserver/publication.go:16`). What breaks, deliberately and before any v2 release: `--ruff/--shellcheck/--no-*/--pyflakes`, `ACTIONLINT_RUFF_*`/`ACTIONLINT_SHELLCHECK_*`, Action inputs `ruff/shellcheck/pyflakes`, `analysis.Options.{ShellCheck,Ruff}`, the Ruff launcher grammar (`ruff/command.go:8-259`: users write `backend.process.{executable,args}` literally, as `docs/config.md:346-352` already shows for ShellCheck). `docs/migration-v2.md` gets the table.

**K6. Scope for this Go release vs later.** **This release: stages 0-6 plus the host side of stage 7.** Later (next Go release or the Rust host): retire the managed route, rebuild the ShellCheck/Ruff components against 0.3.0, OS sandboxing, moving alias/identity to an OCI-native index, Rust-host parity tests over the same manifests (ADR-0008 wants no tool code; manifests are the data it will consume).

**K7. Three masking implementations.** **Decision: one lexer-based masker (`backend.go:227-251`) in `profile/mask`, used by the shell and generic profiles; Python keeps its token-aware sanitizer.** ShellCheck's naive first-`}}` scan (`analysis_shellcheck_run.go:86-119`) differs for expressions containing `}}` inside strings; this is a deliberate behavior change covered by a fixture in stage 3.

**K8. Where defaults come from.** **Decision: bundled manifests in `providers/` at the module root (`embed.go` exposing `FS`), iterated by directory; `config init` keeps installing registry aliases.** `go:embed` cannot reach parent directories, so the data lives beside its tiny public embedding package rather than under `internal/`. The docs generator (`rulecatalog/check-docs`, investigator-cited) iterates the embedded manifests instead of requiring named binaries.

**K9. Required vs optional.** **Decision: no manifest field.** Explicit selection (flag, env, API, repo backend) → required; bundled default → optional (skip + warn), which is today's effective behavior for both tools (`env.go:98-99`; `shellcheck/registry.go:36-39` returns false when options are nil; `composition.go:34-37`).

## 4. Staged plan (each stage green on `go test ./...` with ShellCheck + Ruff 0.17.0 installed, `ci.yml:133-167`, and `make lint` incl. `actionlintarch`)

**Stage 0 — Manifest, jq decoder, bundled data (nothing wired).**
Files: new `internal/tools/provider/{manifest.go,validate.go,manifest_schema.json}`, `internal/tools/provider/process/decoder_jq.go` (gojq, limits: no env, no modules, no inputs, context deadline, output cap), `decoder_envelope.go`, `decoder_sarif.go`, `providers/{embed.go, shellcheck/manifest.json, shellcheck/output.jq, ruff/manifest.json, ruff/output.jq, ruff/config.jq}`; `go.mod` adds gojq.
Tests: every bundled manifest validates against the schema; golden decode tests `providers/<id>/testdata/{stdout.json → diagnostics.json}` captured from real `shellcheck -f json1` / `ruff --output-format json` output; jq sandbox tests (env/import/input refused, timeout, cap); SARIF fixture.
CI: additive only.

**Stage 1 — Config as data.**
Files: `internal/config/config.go:418-446` (map `ToolsConfig`/`ToolConfig`), `shellcheck.go:76-81,98-115`, `files.go:80,85`, `tools/command.go:110-120`, `catalog.go:63,105`, `suppression_scope.go:67`, delete `internal/config/tools/ruff`, `internal/rulecatalog/catalog.go:22,55,78`, `internal/localapp/plugin_cache.go:40-43`, `internal/tools/selection/{module,process,requirements}.go` → `cfg.Tools[name]` (temporary), `internal/architecture/contracts_test.go:22`, `tools/config/main.go` schema generator.
Tests: existing config parse/override tests unchanged in expectation; new: bool shorthand through the map, flat Ruff keys fold into `config`, unknown `tools.<x>` rejected without a registered provider and accepted with one, inherited `tools.*.config` path relativization, JSON-schema output diff.

**Stage 2 — Generic process transport + contract growth + runner absorption.**
Files: `analyzer/backend.go` (+`Source`, `Fixes`), `internal/tools/provider/process/{backend.go,args.go,config_render.go,version.go}`, `internal/analyzerhost/process.go` (+`InheritEnvironment`), `internal/tools/backend/backend.go` (profile hook for run scripts at `:118-130`, fixes, sources, message template), new `internal/tools/backend/{fixes.go,sources.go}` ported from `shellcheck_fixes.go:30-92` and `shellcheck_sources.go`, delete `internal/tools/backend/process.go`, update `internal/application/tools_registry_test.go:24,38` to the new transport.
Tests: args placeholder table; `toml-pairs` rendering equals today's `native_options.go` output (port `native_options_test.go`); version probe (min/exact); env scrubbing + passthrough; generic fix mapping (port `shellcheck_fixes_test.go`); reported-source finding provenance; process integration via the test-binary helper pattern (`tools_registry_test.go:53-72`, `config_tool_command_test.go:20-51`) for envelope, jq and SARIF decoders.

**Stage 3 — Preparation profiles.**
Files: new `internal/tools/profile/{profile.go, mask/, shell/, python/}`; move `shellcheck_shell.go`, `shellcheck_script.go:44-113`, `prepared_plugin.go`, `path_context.go` → `profile/shell`; `ruff/checker.go` sanitize/`templateMasks`/python-shell/target inference → `profile/python`; `backend.go:227-251` → `profile/mask`. `ruff` and `shellcheck` packages import the profiles (behavior unchanged except K7).
Tests: move `shellcheck_shell_test.go`, `shellcheck_script_test.go`, `prepared_plugin_test.go`, `ruff/sanitize_regression_test.go` and the `*_review_test.go` sanitizer suites unchanged; new K7 fixture (`${{ '}}' }}` inside a string).

**Stage 4 — Register from manifests; delete the bespoke adapters.** (Split 4a Ruff, 4b ShellCheck if the diff is too large.)
Files: new `internal/tools/provider/{registration.go,selection.go,bundled.go,managed/}`; `analysis/analysis.go:141-196`, `analysis/rules.go:24`, `internal/githubaction/lint.go:127-132`, Playground wiring → `provider.Bundled()` + `Tools map`; `internal/finding/unavailable_source.go:16-52` producer → id; delete `internal/tools/adapters`, `internal/tools/selection`, `internal/tools/ruff/*`, `internal/tools/shellcheck/*` (managed glue moves to `provider/managed`, generic over `Manifest.Integration`).
Tests: **parity gate**: before deleting, capture snapshots from the current adapters (the `capture_fixture_test.go` pattern already exists in `ruff/`, `shellcheck/`, `backend/`) and assert identical findings (message, code, severity, start/end, fixes, foreign sources, suppression origin) through the manifest route; re-point `internal/integration/tools/*` (`composite_ruff_test.go`, `ruff_process_test.go`, `shellcheck_{fixes,paths,runner,shell,sources}_test.go`, `config_tool_command_test.go:63-77`) from `adapters.Options{...}` to `provider.Bundled()`; `internal/integration/cli/plugin_test.go:88-161` (`ACTIONLINT_TEST_{RUFF,SHELLCHECK}_WASM`) unchanged.

**Stage 5 — Surfaces + 11770d95 disposition.**
Files: `internal/cli/root.go:236-256`, `env.go:83-125`, `plugin.go:68-95`, `doctor.go:54-84,120-140`, `help.go:249-251`, `check.go:49`; `analysis/analysis.go:40-67`; `internal/githubaction/{inputs.go:103-126, lint.go:67-106,155-157, main.go:10-23, outputs.go}` + `action.yml`; `internal/localapp/{config_migration.go:19-54,121-125, plugins.go:31-82,195-213, plugin_selection.go:21-46,106-113}`; delete `internal/localapp/plugin_languages_test.go`; docs `config.md`, `usage.md`, `env.md`, `action.md`, `migration-v2.md`, `plugins.md`.
Tests: CLI `--tool/--no-tool` parsing incl. `=false`, env prefix per id, doctor JSON snapshot with a manifest probe, Action input tests, `TestDisabledToolIsNotResolved` (no `artifact.Resolve` call for a disabled id), public API example tests.

**Stage 6 — Store: OCI + executables + lock v2.**
Files: `internal/plugins/artifact/{resolve.go (oci scheme), oci.go, inspect.go (Kind binary/manifest; Sniff only for wasm kinds), store.go (Materialize)}`, `internal/pluginregistry/{model.go, lock.go}`, `internal/localapp/{plugins.go (dispatch on Manifest.Transport), plugin_cache.go}`, `internal/cli/plugin.go` (install/inspect OCI refs), `plugin/plugin.go` aliases, docs `plugins.md`.
Tests: `httptest` OCI distribution fixture (index → platform manifest → blobs, bearer challenge), digest mismatch, tag reference refused in lock, platform selection, Windows filename materialization (`.exe`; `os/exec` extension handling is **inferred**), `plugin sync` offline from cache, `CachePlan` lists binary digests, lock v1 → v2 round trip.

**Stage 7 — Wire-level unified snippet contract (host side).**
Files: `crates/actionlint-extension-api/wit/script-engine.wit` → `@0.3.0` (+`fixes`, `source`, `resources` negotiate), `core-json-abi.md` check 3, `internal/plugins/component/{script_engine.go,validation.go:34-65,contract.gen.go (regen)}`, `internal/plugins/core/engine.go` negotiation, SDK crate; 0.2.0 still accepted.
Tests: 0.3.0 fixture components under `testdata/plugins` (Rust + wat), negotiation and version-mismatch tests, parity between the process route and a 0.3.0 component on the same stdout fixture.

Order rationale: 0-3 are additive or pure moves, 4 is the single behavior-parity gate protected by captured snapshots, 5 changes public surface only after the engine is data-driven, 6 adds distribution without touching analysis, 7 touches guests last so no guest rebuild blocks the release.
