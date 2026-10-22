# Go library API

The Go library has one module, `actionlint.kjanat.dev`, with packages for distinct responsibilities. The v1.18.0 API
intentionally breaks the former flat root API. The command-line interface and its output formats are separate contracts.

| Package                            | Responsibility                                                        |
| ---------------------------------- | --------------------------------------------------------------------- |
| `actionlint.kjanat.dev`            | Convenience `Analyze` entry point                                     |
| `actionlint.kjanat.dev/analysis`   | Source requests, file/repository discovery and results                |
| `actionlint.kjanat.dev/analyzer`   | Analyzer input, required facts, registration and returned diagnostics |
| `actionlint.kjanat.dev/document`   | Workflow/action outlines and structured action references             |
| `actionlint.kjanat.dev/diagnostic` | Diagnostics, source positions and suggested edits                     |
| `actionlint.kjanat.dev/config`     | Configuration values, loading, composition and inspection             |
| `actionlint.kjanat.dev/syntax`     | Workflow syntax model and parser                                      |

## Analyze loaded sources

```go
result, err := actionlint.Analyze(ctx, analysis.Request{
    Sources: []analysis.Source{{
        Path: "workflow.yml",
        Content: source,
        Config: cfg,
    }},
})
```

`Sources` supplies content and optional configuration explicitly. `Analyze` does not discover configuration files.
`Path` selects workflow or action-manifest parsing and provides diagnostic locations. Findings are returned in
`result.Diagnostics`; a finding does not itself produce a Go error. An error means the analysis was incomplete, and a
non-nil result may still contain useful findings and document outlines. Always inspect both values.

`result.FileCount` includes clean documents. `result.Documents` uses `document.Outlines`. The `document` package can
encode and decode workflow/action outlines and parse `uses` references without importing the parser or execution engine.
Outlines contain selected declarations.
Positions are one-based Unicode character positions; range ends are exclusive. The library result can be marshalled to
JSON, but it is not the CLI's versioned result envelope.

## Analyze files or a repository

```go
result, err := analysis.Files(ctx, paths, analysis.FileOptions{
    Options: analysis.Options{WorkingDir: directory},
    ConfigFile: "actionlint.yaml",
})
```

`analysis.Repository(ctx, directory, options)` discovers workflows and action manifests. `FileOptions` adds configuration
selection, overlays and `SkipProjectConfig` to the shared analysis options. These discovery settings do not appear in
in-memory requests.

Construct invocation overlays with `config.ParseOverlay("config", source)` for a complete document, or
`config.ParseOverlay("tools", source)` for one configuration section. Assign them to `FileOptions.ConfigOverlays`.
`config.DisallowSuppressions("all", "expression")` constructs an enabled policy for
`Config.Policy.DisallowSuppressions`; it validates the report mode and selected rules.

`Options.ReadFile` supplies a concurrent-safe file reader for files read by actionlint. It does not sandbox external tools.
External tools are disabled by default in the library. Enable them explicitly with `Options.ShellCheck` or `Options.Ruff`:

```go
options.Ruff = analysis.Tool{Process: &analysis.Process{Executable: "ruff"}}
```

`Executable` is a literal executable name or path. `Arguments` supplies separate arguments; no shell command-line parsing
is performed. An empty executable disables that tool. CLI environment discovery does not configure a library call.

## Add an analyzer

Implement the following contract and supply it through `Options.Analyzers`:

```go
type Analyzer interface {
    Descriptor() analyzer.Descriptor
    Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error)
}
```

A descriptor names the analyzer and declares required facts: source content, declared run scripts, resolved mapping
names, scripts invoked through composite action calls, or runtime execution facts. Each registration can supply JSON options. The host rejects unsupported or unavailable facts before invocation.
Scripts expose their declared shell; omitted declarations remain unknown. Mapping entries distinguish known, unknown and
invalid keys and retain source text and evaluated-data provenance. `Complete` indicates whether all mapping names are
statically known. A partial parse can supply partial facts; check `Input.ParseStatus` when completeness matters.

`Input.Invoked` lists the scripts of composite actions the document calls. Each names its own `Path` and carries an
`Invocation` with the calling step, the `uses` reference and the nesting depth; findings for such a script name that
path. `Script.Spans` map script text back to the declaring document and `Script.Position` resolves a line and column,
reporting false for text without a source. With the `execution` fact, `Script.Runtime` carries the effective shell and
working directory, each with an explicit known flag. The host collects these facts only when a registered analyzer
requires them.

The host gives each invocation independent slices, runs analyzers concurrently, and combines results in registration
order before the normal diagnostic ordering and filtering. Missing diagnostic rule names and paths default to the
analyzer name and input path. Findings and errors from one analyzer do not discard findings from another.

An analyzer must finish its own work before returning and honor cancellation. The same instance may be called
concurrently for different documents. The contract does not expose traversal callbacks, mutable built-in rules,
process schedulers or caches. Built-in visitor adapters remain an internal implementation detail.

The same contract is available to other languages as the WebAssembly component interface described in
[Plugins](plugins.md). The CLI loads such components with `--plugin`.

## Configuration and syntax

Use `config.Parse`, `config.Load` and `config.Inspect` to work with the existing configuration format. Configuration
constructors include `RequireJobTimeout`, `RequireJobTimeoutRange` and `RequirePermissions`.
`syntax.Parse` returns a workflow and structured parser diagnostics. Parsing does not execute semantic checks.

The configuration and syntax packages alias standalone implementations that do not import the analysis engine. Their
public names are the supported import surface. Treat unexported state as opaque and use keyed struct literals.

## Migration from the flat API

| Previous root API                                          | Replacement                                                                     |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `Analyze` with `AnalysisRequest` / `SourceUnit`            | `Analyze` with `analysis.Request` / `analysis.Source`                           |
| `AnalysisSession.Files` / `Repository`                     | `analysis.Files` / `analysis.Repository`                                        |
| `AnalysisResult.FileCount()`                               | `analysis.Result.FileCount`                                                     |
| `DocumentOutline`, `WorkflowOutline`, `ActionOutline`      | `document.Outline`, `document.Workflow`, `document.Action`                      |
| `Error`, diagnostic output fields                          | `diagnostic.Diagnostic`                                                         |
| `Config`, `ParseConfig`, `ReadConfigFile`, `InspectConfig` | `config.Config`, `Parse`, `Load`, `Inspect`                                     |
| `Workflow`, `Job`, `Step`, `Parse`                         | `syntax.Workflow`, `Job`, `Step`, `Parse`                                       |
| `Rule`, `RuleBase`, `Visitor`, `OnRulesCreated`            | `analyzer.Analyzer` in `Options.Analyzers`, limited to declared facts           |
| `Linter`, `AnalysisRenderer`, CLI output selection         | Run the CLI for its presentation contract; consume library diagnostics directly |
| Expression checker internals, process and cache types      | Internal implementation; no compatibility re-export                             |

The architecture decision and remaining implementation boundaries are recorded in
[the analysis contracts ADR](design/analysis-contracts.md).

### Script engine backends

`analysis.Tool` selects either `Process` or an `analyzer.ScriptBackend`. Supplying both is an error;
leaving both absent disables native execution. When `Options.LoadScriptEngine` is set, repository-selected
Wasm engines can run without a native tool. An explicit empty `Process` also disables configured Wasm engines. `Process.Arguments` is a literal launcher prefix, for example
`mise exec -- ruff`. Configure Ruff rules through `tools.ruff.config`.
Environment overrides and working directories belong to the process adapter.

A backend implements `Check(context.Context, analyzer.ScriptRequest)` and returns script diagnostics
plus an optional failure. Requests contain a virtual filename, sanitized source, and engine configuration
as JSON. Positions count Unicode scalar values from one; end positions are exclusive. Diagnostics may
accompany a failure. The host maps them into the workflow/action source, applies suppressions, and joins
all started work. A backend must honor cancellation and permit concurrent calls; callers own its lifetime.
It may implement `ScriptConfigValidator` to validate its engine configuration before checking a snippet.

`plugin.LoadEngine` returns such a backend for a core WebAssembly module that follows the
actionlint-plugin-ruff JSON ABI, and `plugin.Load` returns an `analyzer.Analyzer` for a component plugin.
Both report their limits. See [Plugins](plugins.md). Repository configuration can select an engine module
through `tools.<name>.backend.wasm`; library tool settings take precedence over it, as they do for
configured processes.

Supply `Options.LoadScriptEngine` to authorize configuration-selected Wasm engines in `Analyze`, `Files`, or
`Repository`. The callback receives resolved module paths, for example:

```go
options.LoadScriptEngine = func(ctx context.Context, path string) (analyzer.ScriptBackend, error) {
    return plugin.LoadEngine(ctx, path, plugin.EngineOptions{})
}
```

Each analysis loads a path once, shares it between documents, and closes loaded engines before returning.
Cleanup errors accompany the result. The loader owns artifact access and runtime limits; `ReadFile` does
not constrain its filesystem access. Supplying a loader does not enable repository-selected processes.
Explicit `Tool.Backend` instances remain caller-owned.

Ruff receives its native options (`target-version`, `lint`, etc.). The native process adapter implements
this same contract. ShellCheck backends receive `shell`, `exclude`, and `external-sources: false`; the
source includes resolved inline directives and runner startup options. That backend route has no external
source or rc-file capability: requests for those features fail explicitly. Native ShellCheck retains its
filesystem-aware integration, including sourced-file diagnostics and fixes.

These Go types define the in-process host API. A core-WASM adapter can translate them to a versioned
plugin operation; a future component adapter can use WIT types without imitating process arguments.
No WASM runtime or component loader is included by this API change.
