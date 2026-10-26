# Architecture enforcement

`actionlintarch` is enabled in the default golangci-lint configuration. Both
`.tool-versions` and `.custom-gcl.yml` pin golangci-lint 2.15.0. `make lint-build`
rebuilds the module plugin and the standalone analyzer, and clears golangci-lint's result cache so local plugin edits cannot reuse stale diagnostics. `make lint` runs the
custom binary and the standalone checker; Zed uses the custom binary after that
build. The analyzer is a separate Go module, so it does not add lint dependencies
to the distributed application.

The Architecture workflow runs on every PR without path filters or skip trailers.
Analyzer correctness and product enforcement have separate jobs. The latter
must fail on current debt: it has no baseline, expected-failure inversion,
`continue-on-error`, or new-issues-only filter. The standalone runner uses the
same analyzer without golangci-lint's `nolint` and exclusion processing. Generated
production Go is checked there too. Ordinary Go build selection still applies;
platform/build-tag coverage requires the corresponding CI lanes.

## Rules

| Code | Enforcement |
| --- | --- |
| ALA100 | Exact package inventory. A new package requires an explicit policy update; new children do not inherit registration. This records package placement, not proof of semantic ownership. |
| ALA101 | Forbidden domain dependencies through project import chains, including aliases and helper packages. Initial boundaries come from the existing architecture tests, which remain in place. |
| ALA102 | Portable domains cannot import host-effect or parser packages directly or through project helpers. Test harness effects (such as reading fixtures) are allowed; test imports still obey domain dependency rules. Standard-library implementation details are not traversed; `net/url`, `net/netip` and `net/textproto` are allowed value operations. |
| ALA103 | Nonempty construction and direct field writes of source references, invocation projections and runtime facts are restricted to declared owners/wire adapters, resolved using Go types rather than spelling. Empty absence/error values are allowed. |
| ALA104 | Retired engine facades and the four duplicated identity fields on execution.Scope are rejected. These rules deliberately expose the unfinished migration on master. |
| ALA105 | Background/TODO context references outside explicit entrypoint/generator packages are rejected, including import aliases and storing the function in a variable. |

The standard `contextcheck` and `exhaustive` linters are also enabled. Enum default
branches do not count as exhaustive. These are additional checks, not a claim of
complete interprocedural cancellation or effect verification.

## Verification

Run `make test-analyzers` for positive and negative fixtures, including renamed
imports, type aliases, indirect forbidden dependencies, allowed owners,
standalone test binaries and an attempted nolint suppression. Run
`make lint-architecture` to inspect the entire current module. Its nonzero status
is intentional while migration debt remains. No existing product implementation
is repaired or excluded by this change.

The checker does not prove semantic non-duplication, concurrency correctness or
absence of effects behind arbitrary interface calls. It does not trace arbitrary
pointer aliases for field mutation. Private fields and opaque constructors should
enforce ownership in the compiler as migration proceeds. Integration/regression
tests remain required for behavior and full consumer migration.

## Protecting the check

Require **Architecture analyzer tests** and **Architecture enforcement** in the
repository ruleset. This PR supplies those checks; it does not modify repository
administration settings. Changes to this module, package inventory, lint config,
Makefile and workflow require independent review. A candidate-controlled workflow
cannot protect itself against someone deleting or weakening it. Once adopted,
run trusted base-policy validation or require approval for policy changes; do not
treat a modified analyzer's green result as acceptance of its own policy change.
