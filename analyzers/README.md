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
| ALA101 | Reject dependency paths from a domain to a forbidden package.                        |
| ALA102 | Reject host and parser dependencies in portable domains.                             |
| ALA103 | Restrict construction and field mutation to each type's declared owners.             |
| ALA104 | Reject the old engine facades and duplicated invocation fields in `execution.Scope`. |
| ALA105 | Restrict `context.Background` and `context.TODO` to entrypoints and generators.      |

`contextcheck` checks context propagation. `exhaustive` requires explicit enum
cases, including switches with a default branch.

## Dependency boundaries

`depguard` can enforce direct import restrictions. ALA101 and ALA102 also trace
imports through project packages. For example, `expression -> helper -> config`
fails with the full dependency path. The check applies the domain's restriction
to that entire path. It uses source-import facts shared between analysis passes.

The policy specifies forbidden dependencies. It leaves ownership within a package
and unlisted dependencies to code review. Standard-library internals are outside
the traversal. URL, address and header value operations are allowed. Tests may
read fixtures; their imports still obey the domain restrictions.

ALA103 uses resolved Go types, including aliases and inferred literal types.
It checks field assignment, increment and decrement. Empty values are allowed.
Pointer alias analysis and synchronization require separate verification.

## Run

- `make test-analyzers`: run the valid and invalid fixtures under `testdata`.
- `make lint`: build and run the default lint configuration.
- `make lint-architecture`: run the analyzer directly, with suppression comments ignored.

The Architecture workflow runs on every PR. Analyzer tests and product enforcement
have separate jobs. Current product violations fail enforcement. Generated Go is
included in the direct run. Build tags and platform selection follow Go's package
loading rules.

Require **Architecture analyzer tests** and **Architecture enforcement** in the
repository ruleset. Policy, analyzer and workflow changes need independent review.
