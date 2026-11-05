# Checking contracts

Host Go implementation lives under `internal/`. The Go plugin SDK contains guest
bindings, not a public API for embedding the host. Plugins use the contracts under
`plugins/protocol`; CLI and LSP code use the private checking operation.

## Inputs and language behavior

`internal/input/workspace` selects sources and configuration. `internal/input/source`
identifies the captured bytes and revision. A display path alone does not establish
which source a finding or edit describes.

`internal/languages/yaml` decodes YAML and maps scalar text to source positions.
`internal/model/parsing` decodes workflows and action manifests into separate
models. Workflow and action definitions are distinct from their invocations:
two calls to one action can have different inputs, reachable steps and findings.

Expression syntax, types, values, function contracts, type checking and evaluation
live in separate packages under `internal/languages/expressions`. JSON expression
values use `internal/languages/json`. Parsing failure, unknown values, evaluation
failure and rule findings are different outcomes. A function's declared return
type does not disappear because one call fails during evaluation.

## Rule declarations and selection

Each built-in rule owns its declaration and implementation under
`internal/checking/rules/<category>`. Its declaration supplies its name, aliases,
deprecation status, category, defaults, options and configuration scope.
`internal/checking/rule_index.go` assembles those declarations. Bundled external
tools supply their declarations through manifests.

Configuration loading receives a `configuration.RuleSet`. It does not import the
implementations. `checking.ConfigurationRules` assembles the declarations for
native and editor callers. Schema generation and option validation use those
declarations instead of maintaining another rule list.

`internal/checking/selection` selects implementations and supplies resolved
configuration. `internal/checking/executor` adapts models to checks and coordinates
nested calls. `internal/checking/passes` runs synchronous workflow passes through
the contract in `internal/checking/traversal`.

## Shared checker inputs

`internal/checking/inputs.Checker` has two operations: `Descriptor` declares the
check and its required inputs; `CheckDocument` receives one captured source and
returns findings together with an optional error. Implementations finish child
work before returning and honor cancellation. The host can call a checker
concurrently for different sources.

Inputs can include extracted scripts, mappings, dependencies and an invocation
graph. Declared scripts belong to the checked source; invoked scripts identify
the action sources that supplied them. An analysis directory is not a filesystem
grant. Read-only filesystems are supplied separately for one call.

`internal/checking/scripts` prepares and executes script-check batches.
`internal/plugins/execution/preparation` owns masking, shell preparation, language
settings and mappings to the original source. Preparation does not launch tools.
`internal/integrations/process` owns process limits and completion.

## Plugin contracts

[`plugins/protocol/wit/analyzer.wit`](../../plugins/protocol/wit/analyzer.wit)
defines the component checker contract. The host converts between that contract
and its private checker inputs. `describe` returns metadata and input requirements;
`analyze` returns diagnostics and an optional explanation of incomplete analysis.

`internal/plugins/manifests` validates manifests. `internal/plugins/artifacts`
resolves and verifies artifacts. `internal/plugins/execution` loads and invokes
components, core Wasm modules and native processes through their declared
contracts. `internal/plugins/installation` handles project declarations and
registry selection. Installation does not make those host Go packages public.

The SDKs and examples live under `plugins/sdk` and `plugins/examples`. Guest code
must build without importing private host packages. Registry validation uses its
private host adapter instead of expanding the public Go API.

## Findings, suppressions and edits

`internal/results/finding` supplies source-bound findings. `internal/results/policy`
applies resolved severity, configured exceptions, inline suppressions, path filters
and duplicate selection. Its `Processor` receives the same rule declarations used
for configuration. `internal/results/suppression` parses directives and accounts
for their use across invocations of the same source.

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

See [CLI and editor responsibilities](application-boundary.md) for request and
resource ownership, and the [Go dependency graph](../go-forks.md) for fork origins
and module requirements.
