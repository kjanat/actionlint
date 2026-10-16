# Application and CLI boundary

## Decision

The CLI translates command syntax into an application request. The application
owns analysis execution and closes engines it loads before returning a result.
The CLI owns terminal behavior, exit status, summaries and report publication.

```mermaid
flowchart TD
    CLI[internal/cli] --> Local[internal/localapp]
    Local --> App[internal/application]
    Local --> Runtime[component and corewasm runtimes]
    Library[analysis.Files and analysis.Repository] --> App
    App --> Workspace[internal/workspace]
    Workspace --> Config[internal/repoconfig]
    App --> Engine[internal/engine]
    App --> Host[internal/analyzerhost]
    Engine --> Host
    CLI --> Render[internal/render]
    CLI --> Config[internal/repoconfig]
    CLI --> Process[internal/processconfig]
    CLI --> Build[internal/buildinfo]
```

`localapp` supplies the local WASM loaders and owns component-plugin lifetime.
`application` accepts injected analyzer registrations and backend loaders. Neither
the engine nor the public analysis API acquires a dependency on a WASM runtime.
Explicitly supplied backends remain caller-owned; engines created by the loader
are session-owned.

## Responsibilities

- `application.Check` coordinates input preparation, analysis and backend cleanup,
  including the empty-repository convention used by pre-commit. It calls
  `workspace` and `engine.Analyze` separately; it does not use the legacy session.
- `workspace` owns repository discovery, configuration loading and overlays,
  file selection, source reads and configuration provenance. Its `Selection`
  carries resolved sources and all consumed paths, including excluded discovery
  candidates. It has no parser or analyzer dependency.
- `analyzerhost` owns concurrent analyzer scheduling and the pool of loaded
  script backends. It has no traversal or repository-configuration dependency.
- The result contains the rule descriptions and source text needed for rendering.
  Renderers do not inspect engine sessions or traversal state.
- Cleanup runs with cancellation removed from its context. Cleanup errors are
  joined with analysis failures, preserving partial results. Report publication
  occurs only after analysis and cleanup succeed.
- `cli/check.go` coordinates the request and selects the exit status.
  `check_render.go` handles terminal formatting; `report_destination.go` owns
  confirmation, temporary files and atomic publication.
- Configuration creation and the embedded default template belong to
  `repoconfig`. Static rule metadata also comes from its existing catalogue.
- Native command options and executable resolution belong to `processconfig`.
- Release identity belongs to `buildinfo`. Build recipes stamp
  `internal/buildinfo.ReleaseVersion` and `internal/buildinfo.InstallOrigin`.

## Alternatives and trade-offs

Importing the public `analysis` API directly from the CLI would avoid a direct
engine import, but its result intentionally omits the source buffers and rule
metadata required by text and template output. Extending that public contract
with terminal concerns would move the coupling into the library API.

The application result instead uses the existing internal rendering data contract.
This retains source fidelity without exposing the legacy engine result publicly.
It does mean that file-analysis library calls currently prepare rendering data
that the public result view discards.

The legacy engine session and project constructors remain as compatibility
adapters for existing internal callers. They delegate input preparation to
`workspace`; the application path bypasses them. Synthetic repository identities
use `workspace.Identity` without reading configuration.

Within analysis, parsing returns a document, syntax findings and an outline.
`traversalHost` constructs rules and visits the AST, returning script work,
collectors and referenced metadata. The scheduler consumes that prepared work;
it does not walk the AST. The coordinator joins traversal resources before
reading collectors, including on early failures. `diagnosticPolicy` then applies
source-specific suppressions, severity, path filters and deduplication without
access to traversal or scheduling. These phases retain the existing private
source-mapping and diagnostic types inside the engine package.

This keeps the built-in rule visitor adapter intact. Replacing its mutable rule
contract is a separate change; the new phase boundaries do not require a new
plugin protocol or expose the visitor to backend implementations.

## Enforcement

Architecture tests reject direct CLI imports of engine and plugin-runtime
implementations, application dependencies on the CLI or command framework, and
concrete WASM runtime dependencies in the engine, application and public analysis
API. Additional dependency checks keep input preparation independent of analysis
and scheduling independent of traversal/configuration. Tests exercise preparation
of malformed source without parsing, per-batch provenance, and traversal that
collects scripts without executing them. Lifecycle tests cover successful checks, analysis failures, cleanup failures,
combined failures and cancellation. Existing CLI tests cover report publication,
format equivalence, pre-commit discovery and terminal behavior.
