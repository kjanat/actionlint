# Checking contracts

The Go host implementation lives under `internal/`. The Go SDK provides guest
bindings for building plugins against the interfaces in `plugins/protocol`.
The CLI and language server call the private checking code.

## Inputs and language behavior

`internal/input/workspace` selects sources and configuration. `internal/base/sourcetext`
identifies the captured bytes and revision. A display path alone does not establish
which source a finding or edit describes.

`internal/languages/yaml` decodes YAML and maps scalar text to source positions.
`internal/model/parsing` decodes workflows and action manifests into separate
models. Workflow and action definitions are distinct from their invocations:
two calls to one action can have different inputs, reachable steps and findings.

Expression syntax, types, values, function contracts, type checking and evaluation
live in separate packages under `internal/languages/expressions`. JSON expression
values use `internal/languages/runnerjson`. Parsing failure, unknown values, evaluation
failure and rule findings are different outcomes. A function's declared return
type does not disappear because one call fails during evaluation.

## Rule declarations and selection

Each built-in rule owns its declaration and implementation under
`internal/checking/rules/<category>`. Its declaration supplies its name, aliases,
deprecation status, category, defaults, options and configuration scope.
`internal/checking/executor/registration` pairs native declarations with their
implementations in one ordered catalogue. The public rule index and execution
registrations derive from that catalogue. Bundled external tools supply their
declarations through manifests.

The declaration types, rule names, aliases, categories and maturities are
defined in `internal/model/rule`. Packages under `internal/model`,
`internal/input`, `internal/results` and `internal/languages` use them without
importing `internal/checking`. Tool settings schemas, tool provider manifests,
output format names, plugin references and the checker contract in
`internal/model/checkinputs` are defined in `internal/model` too, so these
packages do not depend on `internal/plugins`, `internal/output`,
`internal/cli` or `internal/lsp` either. `internal/checking` hosts plugins, so
it may import `internal/plugins`; no `internal/plugins` package imports
`internal/checking`.
`TestLowerLayersDoNotDependOnHigherLayers` in
`internal/development/tests/integration/layering` fails if production code
breaks either rule, directly or through another package.

`checking.ConfigurationRules` collects rule declarations for the CLI and editor
integrations. Configuration loading receives these declarations as a
`configuration.RuleSet`, without importing the rule implementations. The schema
generator and option validator use these declarations too.

`internal/checking/selection` selects implementations and supplies resolved
configuration. `internal/checking/executor` adapts models to checks and coordinates
nested calls. `internal/checking/workflowvisit` defines the visitor and invocation
contracts and runs synchronous workflow passes through them.

Configuration overlays keep values and source locations in one located document.
Merge behavior belongs to the configuration field declaration, including lists
merged by an identifier. Inheritance and matching file overrides use the same
overlay operation. Selecting action rules preserves the other lint settings.

`internal/input/configuration/rulebinding` prepares each rule's caller, source or composite
configuration once for an occurrence. Execution and publication use that prepared
decision. Two calls to the same remote action can therefore retain different
settings. The execution environment captures the assessment date once per check.

## Shared analyzer inputs

`internal/model/checkinputs.Analyzer` has two operations: `Descriptor` declares the
check and its required inputs; `AnalyzeDocument` receives one captured source and
returns findings together with an optional error. Implementations finish child
work before returning and honor cancellation. The host can call an analyzer
concurrently for different sources.

Inputs can include extracted scripts, mappings, dependencies and an invocation
graph. Declared scripts belong to the checked source; invoked scripts identify
the action sources that supplied them. An analysis directory is not a filesystem
grant. Read-only filesystems are supplied separately for one call.

`internal/checking/scriptanalysis` prepares and executes script-check batches.
`internal/externallinters/preparation` owns masking, shell preparation, language
settings and mappings to the original source. Preparation does not launch tools.
`internal/integrations/hostprocess` owns process limits and completion.

Analyzer inputs, descriptors and returned diagnostics have owned copies at the
extension boundary. Each tool callback receives its own configuration copy.
The host retains separate configuration for other tools and publication.
Immutable invocation handles and explicit execution capabilities remain shared.

## Plugin contracts

[`plugins/protocol/wit/analyzer.wit`](../../plugins/protocol/wit/analyzer.wit)
defines the component checker contract. The host converts between that contract
and its private checker inputs. `describe` returns metadata and input requirements;
`analyze` returns diagnostics and an optional explanation of incomplete analysis.

`internal/model/toolmanifest` validates manifests. `internal/plugins/artifacts`
resolves and verifies artifacts. `internal/plugins/pluginexecution` loads and invokes
components, core Wasm modules and native processes through their declared
contracts. `internal/plugins/pluginregistry` resolves and verifies registry
releases. `internal/plugins/installation` resolves and saves a project's plugin
declarations. `internal/plugins/pluginselection` chooses the plugins an
invocation runs. `internal/plugins/configmigration` upgrades legacy
configuration and declares its default plugins. `internal/plugins/pluginacquisition`
locates the plugin caches. None of these packages makes the host Go packages
public.

The SDKs and examples live under `plugins/sdk` and `plugins/examples`. Plugin
code must compile without importing private host packages. The registry tests
plugins through its own unpublished adapter, which can import those packages.

## Findings, suppressions and edits

`internal/results/finding` supplies source-bound findings. `internal/results/policy`
applies resolved severity, configured exceptions, inline suppressions, path filters
and duplicate selection. Its `Processor` receives the same rule declarations used
for configuration. `internal/results/suppression` parses directives and accounts
for their use across invocations of the same source.

`policy.Publish` runs after source captures have closed and been validated. It
evaluates the raw batches together, reconciles suppression use and incomplete
checks, and chooses the retained findings. Required policy decisions are private
to that evaluation. A producer cannot make its diagnostic mandatory by setting
public report fields.

Native adapters forward the complete internal finding. For example, another
language runtime check can emit lifecycle evidence through its declaration check;
the existing policy evaluator and JSON, SARIF and terminal renderers consume it.

`internal/results/edits` plans changes and checks conflicts and source revisions.
`internal/output` renders reports and writes approved edit plans. The LSP returns
workspace edits for the editor to apply. Both paths must reject edits against a
source revision that differs from the one the finding describes.

`internal/results/report` carries completed or partial results without live
execution state. A finding does not prove that every requested check completed;
callers must also handle the returned error.

## Tests and generated files

Unit tests and fixtures sit beside their implementation. Cross-subsystem tests
live under `internal/development/tests/integration`. Runner and Wasmtime comparisons
live under `internal/development/tests/conformance`. Each suite owns its fixtures.

Go generators that read host types or rule declarations live under
`internal/development/generators`. WIT binding tools are declared in the root
Go module and invoked through mise tasks. The guest SDK has its own runtime-only
module. Release and maintenance scripts live under `tools`.

Ownership copies are generated from the owning Go types. Explicit shared-field
contracts identify immutable handles or execution capabilities; unsupported
mutable fields stop generation. The analysis report declares how each field
combines across projects, and its generated merge rejects unclassified fields.
The lint tasks check both sets of generated files for freshness.

See [CLI and editor responsibilities](application-boundary.md) for request and
resource ownership, and the [Go dependency graph](../go-forks.md) for fork origins
and module requirements.
