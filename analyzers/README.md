# Architecture enforcement

`actionlintarch` is enabled in the default golangci-lint configuration. Both
`.tool-versions` and `.custom-gcl.yml` pin golangci-lint 2.15.0. `make lint-build`
rebuilds the module plugin and the standalone analyzer, and clears golangci-lint's result cache so local plugin edits cannot reuse stale diagnostics. `make lint` runs the
custom binary and the standalone checker; Zed uses the custom binary after that
build. The analyzer is a separate Go module, so it does not add lint dependencies
to the distributed application.

## Checks

| Code   | Rule                                                                                 |
| ------ | ------------------------------------------------------------------------------------ |
| ALA100 | Reject packages without an architecture layer.                                       |
| ALA101 | Reject dependencies outside the layer's permitted graph.                             |
| ALA102 | Reject host and parser dependencies in portable layers.                              |
| ALA103 | Restrict construction and field mutation to each type's declared owners.             |
| ALA104 | Reject the old engine facades and duplicated invocation fields in `execution.Scope`. |
| ALA105 | Restrict `context.Background` and `context.TODO` to entrypoint layers.               |

`contextcheck` checks context propagation. `exhaustive` requires explicit enum
cases, including switches with a default branch.

## Dependency contract

[`architecture.json`](../architecture.json) declares the repository's layers,
package roots and allowed dependency directions. The contract follows the
[analysis ownership](../docs/design/analysis-ownership.md) and
[application boundary](../docs/design/application-boundary.md) decisions. Its
assignments describe the target responsibilities; existing violations must be
fixed in the product, not added as exceptions.

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
It checks field assignment, increment and decrement. Empty values are allowed.
Pointer alias analysis and synchronization require separate verification.
Tests may read fixtures and import packages in a `testonly` layer, such as
`internal/testutil`. Those helpers can exercise the application. Other test
imports still obey the dependency contract; production packages cannot import
test support.

## Run

- `make test-analyzers`: run contract checks and valid/invalid fixtures under `testdata`.
- `make lint`: build and run the default lint configuration.
- `make lint-architecture`: run the analyzer directly, with suppression comments ignored.

The Architecture workflow runs on every PR. Analyzer tests and product enforcement
have separate jobs. After building the custom runner, CI also tests a contract-only
change against a warm cache and verifies that a missing contract fails startup.
To repeat that integration test locally, run
`ACTIONLINT_CUSTOM_GCL="$PWD/custom-gcl" go test -C analyzers ./plugin -run TestContractReload -count=1`
after `make lint-build`.

Current product violations fail enforcement. Generated Go is included in the
direct run. Build tags and platform selection follow Go's package loading rules.

Require **Architecture analyzer tests** and **Architecture enforcement** in the
repository ruleset. Policy, analyzer and workflow changes need independent review.
