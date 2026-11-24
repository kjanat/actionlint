# Analysis contracts

## Inputs and language behavior

`internal/base/sourcetext` identifies the captured bytes and their revision. A display path alone does not identify a source.

## Rule declarations and selection

Each built-in rule keeps its declaration and implementation under `internal/checking/rules/<category>`. `internal/checking/executor/registration` pairs them in one ordered catalogue. The rule index and execution both derive from that catalogue. Bundled external tools declare their rules in manifests.

`TestLowerLayersDoNotDependOnHigherLayers` in `internal/development/tests/integration/layering` lists which `internal/` layers may import which. `go test -short` skips it.

Each configuration field declaration defines its merge behavior. Inheritance and file overrides use the same overlay operation in `internal/input/configuration/overlay.go`.

## Shared analyzer inputs

`internal/results.Analyzer` has two operations. `Descriptor` declares the check and its inputs. `AnalyzeDocument` analyzes one captured source and returns findings and an optional error. Implementations finish child work before returning, honor cancellation and accept concurrent calls for different sources. The host grants read-only filesystem access separately for each call.

The extension boundary copies analyzer inputs, descriptors and diagnostics. Each tool callback receives its own configuration copy.

## Plugin contracts

[`plugins/protocol/wit/analyzer.wit`](../../plugins/protocol/wit/analyzer.wit) defines the component checker. `describe` returns metadata and input requirements. `analyze` returns diagnostics and an optional explanation of incomplete analysis. `internal/model/toolmanifest` validates manifests. Plugin code under `plugins/sdk` and `plugins/examples` must compile without private host packages.

## Findings, suppressions and edits

`internal/results/policy` applies severity, configured exceptions, inline suppressions, path filters and duplicate selection. `internal/results/suppression` parses directives and tracks their use across invocations of one source. `policy.Publish` runs after source captures close and applies policy to all batches at once.

`internal/results/edits` plans changes and checks conflicts and revisions. `internal/output` writes approved edits, and the LSP returns workspace edits to the editor. Both paths reject an edit whose source revision differs from the finding's revision. A report can hold partial results. Callers must handle the returned error as well as the findings.

## Generated files

The `deep-copies` and `analysis-merge` generators under `internal/development/generators` write copy and merge code for shared types. They stop on fields they cannot classify. `run lint` checks that their output is current.
