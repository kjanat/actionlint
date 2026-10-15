# Go library API

The Go library has one module, `actionlint.kjanat.dev`, with packages for distinct responsibilities. The v1.18.0 API
intentionally breaks the former flat root API. The command-line interface and its output formats are separate contracts.

| Package                            | Responsibility                                                            |
| ---------------------------------- | ------------------------------------------------------------------------- |
| `actionlint.kjanat.dev`            | Convenience `Analyze` entry point                                         |
| `actionlint.kjanat.dev/analysis`   | Source requests, file/repository discovery, results and document outlines |
| `actionlint.kjanat.dev/analyzer`   | Analyzer input, required facts, registration and returned diagnostics     |
| `actionlint.kjanat.dev/diagnostic` | Diagnostics, source positions and suggested edits                         |
| `actionlint.kjanat.dev/config`     | Configuration values, loading, composition and inspection                 |
| `actionlint.kjanat.dev/syntax`     | Workflow syntax model and parser                                          |

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

`result.FileCount` includes clean documents. Document outlines are selected declarations, not a complete semantic graph.
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
options.Ruff = analysis.Tool{Executable: "ruff"}
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

The configuration and syntax packages currently expose selected model aliases backed by the internal engine. Their
public names are the supported import surface. Treat unexported state as opaque and use keyed struct literals.

## Migration from the flat API

| Previous root API                                          | Replacement                                                                     |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `Analyze` with `AnalysisRequest` / `SourceUnit`            | `Analyze` with `analysis.Request` / `analysis.Source`                           |
| `AnalysisSession.Files` / `Repository`                     | `analysis.Files` / `analysis.Repository`                                        |
| `AnalysisResult.FileCount()`                               | `analysis.Result.FileCount`                                                     |
| `Error`, diagnostic output fields                          | `diagnostic.Diagnostic`                                                         |
| `Config`, `ParseConfig`, `ReadConfigFile`, `InspectConfig` | `config.Config`, `Parse`, `Load`, `Inspect`                                     |
| `Workflow`, `Job`, `Step`, `Parse`                         | `syntax.Workflow`, `Job`, `Step`, `Parse`                                       |
| `Rule`, `RuleBase`, `Visitor`, `OnRulesCreated`            | `analyzer.Analyzer` and registrations                                           |
| `Linter`, `AnalysisRenderer`, CLI output selection         | Run the CLI for its presentation contract; consume library diagnostics directly |
| Expression checker internals, process and cache types      | Internal implementation; no compatibility re-export                             |

The architecture decision and remaining implementation boundaries are recorded in
[the analysis contracts ADR](design/analysis-contracts.md).
