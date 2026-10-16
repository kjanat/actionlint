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
Outlines are selected declarations, not a complete semantic graph.
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

A descriptor names the analyzer and declares required facts: source content, declared run scripts, or resolved mapping
names. Each registration can supply JSON options. The host rejects unsupported or unavailable facts before invocation.
Scripts expose their declared shell, not an inferred runner default. Mapping entries distinguish known, unknown and
invalid keys and retain source text and evaluated-data provenance. `Complete` indicates whether all mapping names are
statically known. A partial parse can supply partial facts; check `Input.ParseStatus` when completeness matters.

The host gives each invocation independent slices, runs analyzers concurrently, and combines results in registration
order before the normal diagnostic ordering and filtering. Missing diagnostic rule names and paths default to the
analyzer name and input path. Findings and errors from one analyzer do not discard findings from another.

An analyzer must finish its own work before returning and honor cancellation. The same instance may be called
concurrently for different documents. The contract does not expose traversal callbacks, mutable built-in rules,
process schedulers or caches. Built-in visitor adapters remain an internal implementation detail.

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
leaving both absent disables the tool. `Process.Arguments` is a literal launcher prefix, for example
`mise exec -- ruff`; configure Ruff rules through `tools.ruff.config` instead of command flags.
Environment overrides and working directories belong to the process adapter.

A backend implements `Check(context.Context, analyzer.ScriptRequest)` and returns script diagnostics
plus an optional failure. Requests contain a virtual filename, sanitized source, and engine configuration
as JSON. Positions count Unicode scalar values from one; end positions are exclusive. Diagnostics may
accompany a failure. The host maps them into the workflow/action source, applies suppressions, and joins
all started work. A backend must honor cancellation and permit concurrent calls; callers own its lifetime.
It may implement `ScriptConfigValidator` to validate its engine configuration before checking a snippet.

Ruff receives its native options (`target-version`, `lint`, etc.). The native process adapter implements
this same contract. ShellCheck backends receive `shell`, `exclude`, and `external-sources: false`; the
source includes resolved inline directives and runner startup options. That backend route has no external
source or rc-file capability: requests for those features fail explicitly. Native ShellCheck retains its
filesystem-aware integration, including sourced-file diagnostics and fixes.

These Go types are a host API, not a stable wire ABI. A core-WASM adapter can translate them to a versioned
plugin operation; a future component adapter can use WIT types without imitating process arguments.
No WASM runtime or component loader is included by this API change.
