# Architecture enforcement

This Go module contains the standalone `actionlintarch` analyzer and its
golangci-lint plugin. The repository's lint command and Zed configuration use
standard golangci-lint.

## Checks

| Code   | Rule                                                                                 |
| ------ | ------------------------------------------------------------------------------------ |
| ALA100 | Reject packages without an architecture layer.                                       |
| ALA101 | Reject dependencies outside the layer's permitted graph.                             |
| ALA102 | Reject host and parser dependencies in portable layers.                              |
| ALA103 | Restrict construction and field mutation to each type's declared owners.             |
| ALA104 | Reject the old engine facades and duplicated invocation fields in `execution.Scope`. |
| ALA105 | Restrict `context.Background` and `context.TODO` to entrypoint layers.               |

## Dependency contract

[`architecture.json`](../architecture.json) declares the repository's layers,
package roots and allowed dependency directions. The contract follows the
[analysis ownership](../docs/design/analysis-ownership.md) and
[application boundary](../docs/design/application-boundary.md) decisions. Its
assignments describe the target responsibilities. Existing violations require
changes to the product code.

Each layer has `roots` and `uses`. A root covers its entire package subtree;
the longest matching root wins. `.` matches only the module package. `uses`
names the layers it depends on. Packages may use their own layer and every
layer reachable through `uses`. All other project dependencies fail.

For example, traversal uses syntax, syntax uses semantics, and semantics uses
values. A new package under `internal/engine` automatically inherits traversal's
permissions. It cannot import application code. A new unrelated root has no
layer and fails even when it imports nothing. There is no package inventory,
catch-all layer or list of forbidden package pairs.

The checker follows source imports through project helpers. Moving a prohibited
import into a helper does not hide it. Diagnostics include both layer names and
the dependency path. Standard-library internals are outside that traversal.
`portable` rejects the host/parser imports checked by ALA102; `entrypoint`
permits detached root contexts. Nested roots give generators separate
permissions without allowing the surrounding domain to import them back.

The contract reader and graph checks live in `analyzers/architecture` and contain
no actionlint package names. Both runners load the same contract. Missing files,
unknown fields/layers, duplicate roots and graph cycles stop lint startup.
Contract changes also change the analyzer's cache identity, so they take effect
without rebuilding the binary or manually clearing golangci-lint's cache.
The plugin setting `contract` is a path relative to the invocation directory;
the standalone command reads `architecture.json` there. Run both from the
repository root.

This contract controls dependencies between layers. It does not establish that
code inside an allowed package implements the right responsibility. The
remaining type ownership and retired-API checks are actionlint-specific.
ALA103 uses resolved Go types, including aliases and inferred literal types.
It checks field assignments, range assignments, increments and decrements,
including promoted fields and nested value fields such as `inv.Step.Line`.
Conversions from other types into a protected type
also require ownership. Same-type copies, nil pointers and empty literals are
allowed.
Pointer alias analysis and synchronization require separate verification.
Tests may read fixtures and import packages in a `testonly` layer, such as
`internal/testutil`. Those helpers can exercise the application. Other test
imports still obey the dependency contract; production packages cannot import
test support.

## Test this module

From the repository root, run `mise exec -- go test -C analyzers ./...`.
