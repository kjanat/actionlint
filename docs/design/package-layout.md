# Package layout

Issue [#21](https://github.com/kjanat/actionlint/issues/21) asks whether the flat root package should be split into
`ast`, `expr` and `rule`, and names one question as blocking the work: where `Pos` and `Error` end up. This document
answers that question, records the measured dependency graph the answer rests on, gives a target layout that the same
measurement shows is acyclic, and lists what each phase of the move has to do.

Nothing here changes Go code. Phase 0 is this document.

## How the graph was measured

Load the root package with `golang.org/x/tools/go/packages` in a mode that includes `NeedSyntax`, `NeedTypes` and
`NeedTypesInfo`. Assign every top-level declaration, struct field and interface method to its source file, then
attribute every `types.Object` in `TypesInfo.Uses` to the file holding the declaration that encloses the use. Exclude
`_test.go` files, and split `error.go` at symbol level into the `Error` value object and the `ErrorFormatter` side.
Without that split the `{ErrorFormatter, command, linter}` cycle is smeared across three apparent ones.

Every figure below was measured at commit `2f85e20`. Section [Reproducing the measurements](#reproducing-the-measurements)
gives a command for each claim.

## What the root package holds today

82 `.go` files in the repository root, 34163 lines. 46 production files with 20298 lines and 36 test files with 13865
lines. There is no subpackage. In-repository consumers are `cmd/actionlint`, `cmd/actionlint-action`, `playground`
(js/wasm), `fuzz`, `scripts/check-checks` and `scripts/generate-popular-actions`.

### Exported surface

| kind                                   | count   |
| -------------------------------------- | ------- |
| types                                  | 154     |
| top-level funcs                        | 52      |
| consts                                 | 60      |
| vars                                   | 10      |
| **top-level total**                    | **276** |
| methods with an exported name          | 223     |
| of which on an exported receiver       | 220     |
| **total exported identifiers**         | **499** |
| **reachable from outside the package** | **496** |

Three files declare no exported top-level symbol: `process.go` (171 lines), `quotes.go` (91 lines) and `doc.go`
(47 lines, package documentation and no code).

### Test coupling

34 of the 36 test files are white-box `package actionlint`. Only `example_test.go` and `example_your_own_rule_test.go`
are `package actionlint_test`.

The production graph below deliberately excludes tests, but the move cannot defer them. A separate type-based pass over
the test variant finds multiple private symbols used across proposed package boundaries. `error_test.go` tests both the
diagnostic value and the root formatter and must split. The parsing cases in `rule_required_actions_test.go` must become
external `package rule_test` tests: leaving them white-box and importing the root for `Parse` creates
`rule -> root -> rule`. Its helper-only cases move to `internal` or stay as white-box `rule` tests as appropriate. The
other cross-boundary cases must be rewritten against the public APIs in the same phase as their production files.

### Measured dependency graph

`A -> B` means a declaration in A uses a symbol declared in B. The number is the count of distinct symbols.

```text
action_metadata      -> project(2) reusable_workflow(1)
all_webhooks         -> (none)
ast                  -> (none)
availability         -> (none)
command              -> err:value(1) linter(19)
config               -> err:value(2) rule_required_actions(1)
err:formatter        -> command(1) err:value(3) rule(3)
err:value            -> ast(4)
expr                 -> (none)
expr_ast             -> expr_lexer(1)
expr_insecure        -> expr(1) expr_ast(12) expr_sema(1) quotes(1)
expr_lexer           -> expr(5)
expr_parser          -> expr(5) expr_ast(23) expr_lexer(32) quotes(3)
expr_sema            -> availability(1) expr(5) expr_ast(42) expr_insecure(8) expr_lexer(4)
                        expr_type(27) quotes(2)
expr_type            -> (none)
glob                 -> (none)
linter               -> action_metadata(4) config(12) err:formatter(5) err:value(8) parse(1) pass(4)
                        process(3) project(7) reusable_workflow(4) rule(5) rule_action(1)
                        rule_credentials(1) rule_deprecated_commands(1) rule_env_var(1) rule_events(1)
                        rule_expression(1) rule_glob(1) rule_id(1) rule_if_cond(1) rule_job_needs(1)
                        rule_matrix(1) rule_parallel_steps(1) rule_permissions(1) rule_pyflakes(1)
                        rule_require_commit_hash(1) rule_require_job_timeout(1) rule_required_actions(1)
                        rule_runner_label(1) rule_shell_name(1) rule_shellcheck(1)
                        rule_workflow_call(1)
parse                -> ast(246) err:value(5) quotes(1)
pass                 -> ast(11)
popular_actions      -> action_metadata(8)
process              -> (none)
project              -> config(2)
quotes               -> (none)
reusable_workflow    -> ast(18) expr_type(5) parse(1) project(2)
rule                 -> ast(6) config(1) err:value(4) pass(1)
rule_action          -> action_metadata(31) ast(10) popular_actions(2) quotes(1) rule(6)
rule_credentials     -> ast(12) rule(4)
rule_deprecated_commands -> ast(6) rule(4)
rule_env_var         -> ast(18) rule(4)
rule_events          -> all_webhooks(1) ast(47) quotes(4) rule(5)
rule_expression      -> action_metadata(5) ast(175) availability(1) config(1) expr(4) expr_ast(1)
                        expr_lexer(2) expr_parser(2) expr_sema(11) expr_type(20) popular_actions(1)
                        reusable_workflow(6) rule(7) rule_action(1) rule_workflow_call(1)
rule_glob            -> ast(22) glob(5) rule(4)
rule_id              -> ast(11) rule(4)
rule_if_cond         -> ast(10) expr_lexer(1) expr_parser(2) expr_sema(2) rule(4)
rule_job_needs       -> ast(10) rule(5)
rule_matrix          -> ast(33) quotes(1) rule(5)
rule_parallel_steps  -> ast(20) rule(4)
rule_permissions     -> ast(11) quotes(2) rule(4)
rule_pyflakes        -> ast(15) process(6) rule(5) rule_shellcheck(1)
rule_require_commit_hash -> ast(11) rule(4) rule_action(1) rule_workflow_call(1)
rule_require_job_timeout -> ast(10) config(3) rule(4)
rule_required_actions -> ast(13) config(1) rule(5)
rule_runner_label    -> ast(22) config(2) expr_ast(5) expr_lexer(1) expr_parser(2) quotes(1) rule(5)
rule_shell_name      -> ast(17) quotes(1) rule(4)
rule_shellcheck      -> ast(20) process(6) rule(6)
rule_workflow_call   -> ast(17) quotes(1) reusable_workflow(10) rule(6) rule_action(1)
```

The graph has exactly three strongly connected components with more than one member.

**`{expr_insecure, expr_sema}`.** `expr_insecure.go:278,287` call `errorfAtExpr`, declared at `expr_sema.go:413`, while
`expr_sema.go` uses `UntrustedInputChecker` and five of its methods from `expr_insecure.go`. Both files go into the same
target package, so this cycle does not constrain the split.

**`{err:formatter, command, linter}`.** `error.go:302` puts `getCommandVersion` from `command.go:70` into a
`template.FuncMap`; `error.go:340` takes the `Rule` interface; `command.go:31,166` build the `Linter`; and `linter.go`
holds an `*ErrorFormatter` and calls `NewErrorFormatter`, `Print`, `PrintErrors` and `RegisterRule`. This cycle does
constrain the split, and the layout below resolves it by keeping all three in the root package.

**`{config, rule_required_actions}`.** `config.go:88` calls `invalidActionPattern`, declared at
`rule_required_actions.go:25`, while `rule_required_actions.go:125` calls `Config.RequiredActions`. Moving the three
action-pattern helpers to `internal` removes the first edge; the rule then imports `project`, but `project` imports only
`diag` and `internal`.

Everything else is a directed acyclic graph.

## The blocking decision

**`Pos` stays in `ast`. The `Error` value object moves into its own package. `ErrorFormatter` stays in the root. There
is no separate `pos` package.**

### Why `Pos` stays in `ast`

`Pos` is a two-field struct at `ast.go:14-19` with two methods, `String` at `ast.go:134` and `IsBefore` at
`ast.go:139`. Its only helper is `byteOffsetAtColumn` at `ast.go:117`, called from `scriptSource.lookup` at `ast.go:93`,
from `parse.go:162`, and from `Error.getEndColumn` and `Error.getIndicator` at `error.go:141,164,172`. A dedicated `pos`
package would hold six lines of struct, two methods and one helper, and every package that imports `ast` would import
it as well. No boundary in the layout below needs it.

`ast.go` and `pass.go` reference `ExprNode`, `ExprType` and `ExprError` zero times, so the AST and the expression tree
do not form a cycle. `ast` has zero outgoing edges. It is already a leaf and stays one.

### Why the `Error` value object does not go into `ast`

The `Error` struct at `error.go:26-40` stores `Message`, `Filepath`, `Line`, `Column`, `Kind` and an unexported
`endColumn`. It holds no `Pos`. Its whole dependency on `ast` is four symbols: `Pos`, `Pos.Line` and `Pos.Col` in the
two constructors, and `byteOffsetAtColumn` in the two indicator helpers. Nothing in `ast.go` or `pass.go` refers to
`Error`, so placing `Error` in `ast` is possible. Three measurements argue against it.

1. **It would make `ast` depend on a terminal colour library.** `ast.go` imports five standard packages and
   `go.yaml.in/yaml/v4`. `Error.PrettyPrint` at `error.go:97-122` uses `github.com/fatih/color` through the
   `bold`/`green`/`yellow`/`gray` vars, and reaches `github.com/mattn/go-runewidth` through
   `Error.getIndicator` at `error.go:176,183`. Merging the two puts both libraries in the dependency set of the
   workflow syntax tree.
2. **Two of the four published consumers need the diagnostic and no AST at all.** `cmd/actionlint-action` uses nine
   symbols from that side of `error.go` (`Error` and its fields plus `ErrorTemplateFields`) and zero AST nodes.
   `playground` uses seven and zero. Both name the type explicitly, at `cmd/actionlint-action/lint.go:84`
   (`[]*actionlint.Error`), `cmd/actionlint-action/render.go:12` (`= actionlint.ErrorTemplateFields`) and
   `playground/main.go:20` (`*actionlint.Error`). Putting `Error` in `ast` makes all three read `*ast.Error` and
   `ast.ErrorTemplateFields` in programs that never touch a workflow node.
3. **It would put the whole AST behind `project`.** The diagnostic dependency of `config.go` is exactly two symbols,
   `Error` and `Error.Message`. With `Error` in `ast`, the configuration package imports the workflow syntax tree for
   one struct. Its separate required-action validator dependency moves to `internal` below.

The package is called `diag` below. It sits one layer above `ast`, depends on it for `Pos` and `ByteOffsetAtColumn`,
and nothing else.

### Why `ErrorFormatter` stays in the root

`NewErrorFormatter` at `error.go:302` needs `getCommandVersion` from `command.go`, and `ErrorFormatter.RegisterRule` at
`error.go:340` takes the `Rule` interface and reads `Rule.Name` and `Rule.Description`. That is the one cycle in the
graph that matters, and it disappears when `ErrorFormatter` stays with `linter.go` and `command.go`.

### Where the split runs through `error.go`

`error.go` is 349 lines. Lines 1 to 228 move to `diag`; lines 229 to 349 stay in the root.

| stays in the root                                     | moves to `diag`                                           |
| ----------------------------------------------------- | --------------------------------------------------------- |
| `unescapeBackslash`, `toPascalCase`                   | `Error` and its fields, `Error()`, `String()`             |
| `ruleTemplateFields`, `compareRuleTemplateByName`     | `errorAt`, `errorfAt`                                     |
| `ErrorFormatter`, `NewErrorFormatter`                 | `GetTemplateFields`, `ErrorTemplateFields`, `PrettyPrint` |
| `ErrorFormatter.Print`, `PrintErrors`, `RegisterRule` | `getLine`, `getEndColumn`, `getIndicator`                 |
|                                                       | `compareErrors`, `equalsErrors`                           |
|                                                       | the `bold`, `green`, `yellow` and `gray` vars             |

`unescapeBackslash` and `toPascalCase` have `NewErrorFormatter` as their only caller. The four colour vars have
`PrettyPrint` as their only caller.

## Target layout

```text
actionlint.kjanat.dev              Linter, LinterOptions, Command, ErrorFormatter, Parse
actionlint.kjanat.dev/ast          Pos, AST nodes, Pass, Visitor, ScriptSource
actionlint.kjanat.dev/data         generated tables: availability.go, all_webhooks.go
actionlint.kjanat.dev/diag         Error, ErrorTemplateFields, ErrorAt, ErrorfAt, PrettyPrint
actionlint.kjanat.dev/glob         ValidateRefGlob, ValidatePathGlob, InvalidGlobPatternError
actionlint.kjanat.dev/internal     process.go, quotes.go, action-pattern helpers
actionlint.kjanat.dev/expr         lexer, parser, sema, types, insecure-input checker
actionlint.kjanat.dev/project      Config, PathConfig, Project, Projects
actionlint.kjanat.dev/actions      ActionMetadata, PopularActions, ReusableWorkflowMetadata
actionlint.kjanat.dev/rule         Rule, RuleBase, all rule_*.go
```

Each difference from the layout in the issue is forced by a measured edge.

- **`data` exists** because `expr_sema.go:572` reads `SpecialFunctionNames`, declared at `availability.go:55`. With
  `availability.go` in `rule` that is an `expr -> rule` edge against 46 symbols going the other way; with
  `availability.go` in the root it closes `root -> rule -> expr -> root`. `availability.go` and `all_webhooks.go`
  together have zero outgoing edges, so a leaf package holds them.
- **`project` exists** because the `Rule` interface at `rule.go:115-116` declares `SetConfig(cfg *Config)` and
  `Config() *Config`, and because `action_metadata.go:198,204,254,319` and
  `reusable_workflow.go:172,218,244,374,404` take `*Project` and call `Project.RootDir()`.
- **The required-action pattern helpers move to `internal`.** Current `config.go:88` calls `invalidActionPattern` from
  `rule_required_actions.go:25`, while the rule reads `Config.RequiredActions`. Leaving the helper in `rule` creates
  `project -> rule -> project`. `SplitActionRef`, `InvalidActionPattern` and `MatchGlob` are private module plumbing,
  so both packages can import them from the existing internal leaf without widening the published API.
- **`diag` exists** for the reasons in the previous section.
- **`glob` exists** because `glob.go` has zero outgoing edges and its consumers are `rule_glob.go` (5 symbols) and
  `fuzz`. Nothing in `actions` uses it. `ValidateRefGlob` and `ValidatePathGlob` are documented public API in
  `docs/api.md`, so `internal` would remove them from the published surface.
- **`nodeKindName` moves to `ast`.** `reusable_workflow.go:19` calls it and `parse.go:17` declares it. It is a
  17-line pure function over `yaml.Kind`, and `ast.go:10` already imports `go.yaml.in/yaml/v4`. Leaving it in `parse.go`
  puts an `actions -> root` edge in the graph and closes `root -> actions -> root`.

### The layout is acyclic

Measured edges after the move, counted in distinct symbols:

```text
ast      -> (none)
data     -> (none)
glob     -> (none)
internal -> (none)
diag     -> ast(4)
expr     -> data(1) internal(5)
project  -> diag(2) internal(1)
actions  -> ast(19) expr(5) project(2)
rule     -> actions(47) ast(230) data(2) diag(4) expr(46) glob(5) internal(14) project(8)
root     -> actions(8) ast(251) diag(11) internal(4) project(19) rule(27)
```

Six layers, no strongly connected component with more than one member:

```text
L0  ast   data   glob   internal
L1  diag  expr
L2  project
L3  actions
L4  rule
L5  root
```

### Size of each package

Production files only.

| package    | files | lines                          |
| ---------- | ----- | ------------------------------ |
| `ast`      | 2     | 1334                           |
| `data`     | 2     | 107                            |
| `diag`     | 1     | 228                            |
| `glob`     | 1     | 261                            |
| `internal` | 3     | 288                            |
| `expr`     | 7     | 3212                           |
| `project`  | 2     | 436                            |
| `actions`  | 3     | 6860 (of which 6110 generated) |
| `rule`     | 22    | 4663                           |
| root       | 5     | 2909                           |

`ast` gains 17 lines from `parse.go`, `diag` takes 228 lines from `error.go`, and `internal` gains 26 lines from
`rule_required_actions.go`, so the source-line total stays unchanged.

## Symbols the new boundaries cut

33 unexported symbols are used from a file that lands in a different package. Each has to be exported, or its user has
to change. The list is complete.

### `ast`

| now                                      | used by                                                     | proposal                |
| ---------------------------------------- | ----------------------------------------------------------- | ----------------------- |
| `byteOffsetAtColumn` (`ast.go:117`)      | `parse.go:162`, `diag`                                      | `ByteOffsetAtColumn`    |
| `isExprAssigned` (`ast.go:171`)          | `parse.go:310,321`, `rule_expression.go:1073`               | `IsExprAssigned`        |
| `scriptSource` (`ast.go:36`)             | `parse.go:84,95,150`, `rule_shellcheck.go:171`              | `ScriptSource`          |
| `newScriptSource` (`ast.go:42`)          | `parse.go:126,155`                                          | `NewScriptSource`       |
| `(*scriptSource).mapBytes` (`ast.go:57`) | `parse.go:143,184`                                          | `ScriptSource.MapBytes` |
| `(*scriptSource).pos` (`ast.go:80`)      | `rule_shellcheck.go:235`                                    | `ScriptSource.Pos`      |
| `(*scriptSource).endPos` (`ast.go:84`)   | `rule_shellcheck.go:236`                                    | `ScriptSource.EndPos`   |
| `ExecRun.source` (`ast.go:565`)          | written by `parse.go:1311`, read by `rule_shellcheck.go:66` | field `ExecRun.Source`  |
| `nodeKindName` (`parse.go:17`)           | ten calls in `parse.go`, `reusable_workflow.go:19`          | `NodeKindName`          |

`ExecRun.source` is written on one side of the boundary and read on the other, so a getter alone is not enough. Export
the field. Every other field of `ExecRun` is already exported and `parse.go` builds all nodes field by field.

### `diag`

| now                               | used by         | proposal        |
| --------------------------------- | --------------- | --------------- |
| `errorAt` (`error.go:51`)         | `rule.go:45`    | `ErrorAt`       |
| `errorfAt` (`error.go:60`)        | `rule.go:52,58` | `ErrorfAt`      |
| `compareErrors` (`error.go:187`)  | `linter.go:654` | `CompareErrors` |
| `equalsErrors` (`error.go:200`)   | `linter.go:655` | `EqualsErrors`  |
| `Error.endColumn` (`error.go:39`) | `rule.go:60`    | see below       |

`compareErrors` and `equalsErrors` are production dependencies of `linter.go`, not test-only helpers, so an
`export_test.go` bridge does not cover them.

`Error.endColumn` should stay unexported. Its zero value means "let the formatter derive the range from the source
token at `Column`" (`error.go:37-39`), and a public field invites values that contradict `Column`. Add one exported
constructor carrying the logic that `rule.go:57-63` has today:

```go
// ErrorfRangeAt creates a new Error at the given start position with an inclusive range end derived
// from the exclusive end position. The end is ignored when it is nil or when it does not lie after
// the start on the same line.
func ErrorfRangeAt(start, end *ast.Pos, kind string, format string, args ...any) *Error
```

`RuleBase.errorfRange` then forwards to it and keeps its current signature.

### `internal`

| now                                                                                                                        | used by                                                                 |
| -------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `concurrentProcess`, `newConcurrentProcess`, `concurrentProcess.wait`, `concurrentProcess.newCommandRunner` (`process.go`) | `linter.go`, `rule_pyflakes.go`, `rule_shellcheck.go`                   |
| `externalCommand` and its `exe`, `run`, `wait` (`process.go`)                                                              | `rule_pyflakes.go`, `rule_shellcheck.go`                                |
| `quotes`, `quotesAll`, `sortedQuotes` (`quotes.go`)                                                                        | `expr_sema.go`, `expr_insecure.go`, `parse.go`, seven `rule_*.go` files |
| `quotesBuilder` and its `append`, `build` (`quotes.go`)                                                                    | `expr_parser.go`, `rule_events.go`                                      |
| `splitActionRef`, `invalidActionPattern`, `matchGlob` (`rule_required_actions.go`)                                         | `config.go`, `rule_required_actions.go`                                 |

Go's internal-import rule confines `actionlint.kjanat.dev/internal` to importers under `actionlint.kjanat.dev/`, so
exporting these 17 symbols does not widen the published API. The action-pattern helpers become `SplitActionRef`,
`InvalidActionPattern` and `MatchGlob`.

### `project` and `actions`

| now                                                                     | used by                    | proposal                                |
| ----------------------------------------------------------------------- | -------------------------- | --------------------------------------- |
| `writeDefaultConfigFile` (`config.go:291`)                              | `linter.go:262`            | `WriteDefaultConfigFile`                |
| `(*LocalReusableWorkflowCache).writeCache` (`reusable_workflow.go:193`) | `rule_workflow_call.go:69` | `LocalReusableWorkflowCache.WriteCache` |

## Code generators

Three generators write a hard-coded package clause into their output: `scripts/generate-availability/main.go:176`,
`scripts/generate-webhook-events/main.go:270` and `scripts/generate-popular-actions/main.go:264`.

| generator                  | target file                  | package clause    |
| -------------------------- | ---------------------------- | ----------------- |
| `generate-availability`    | `data/availability.go`       | `package data`    |
| `generate-webhook-events`  | `data/all_webhooks.go`       | `package data`    |
| `generate-popular-actions` | `actions/popular_actions.go` | `package actions` |

`scripts/generate-popular-actions/main.go:23` imports the root package for `ActionMetadata` and 13 related symbols.
That import becomes `actionlint.kjanat.dev/actions`.

The three `//go:generate` directives move with their files: `action_metadata.go:15` to `actions/action_metadata.go`,
`rule_expression.go:8` to `rule/rule_expression.go` and `rule_events.go:12` to `rule/rule_events.go`. Each output path
in a directive is relative and has to be rewritten.

Two places in the `Makefile` need the new paths. `Makefile:59-64` names `popular_actions.go all_webhooks.go
availability.go` as a target and repeats the three names in the `$(TOUCH)` line. `Makefile:5` builds `SRCS` from
`$(wildcard *.go cmd/*/*.go)`, which matches no subdirectory, so `make build` would stop rebuilding after a change in
any new package.

## Consumers

Measured per consumer, showing which target package each currently used root symbol comes from.

| consumer                                           | symbols | after the split                                                            |
| -------------------------------------------------- | ------- | -------------------------------------------------------------------------- |
| `cmd/actionlint`                                   | 5       | 5 root (`Command`, `Command.Main`, `Stdin`, `Stdout`, `Stderr`). No change |
| `cmd/actionlint-action`                            | 26      | 9 `diag`, 17 root                                                          |
| `playground` (js/wasm)                             | 10      | 7 `diag`, 3 root (`NewLinter`, `LinterOptions`, `Linter.Lint`)             |
| `fuzz`                                             | 30      | 16 `rule`, 5 `expr`, 4 `ast`, 2 `actions`, 2 `glob`, 1 root (`Parse`)      |
| `scripts/check-checks`                             | 11      | 8 root, 3 `project` (`NewProjects`, `Projects.At`, `Project.RootDir`)      |
| `scripts/generate-popular-actions`                 | 14      | 14 `actions`                                                               |
| `example_test.go`, `example_your_own_rule_test.go` | 25      | 12 root, 6 `diag`, 4 `rule`, 3 `ast`                                       |

The two example files are the documented extension point and go from one import to four.
`LinterOptions.OnRulesCreated` at `linter.go:93` has type `func([]Rule) []Rule` and becomes
`func([]rule.Rule) []rule.Rule`, which puts `rule` in the root package's public signature. The root imports `rule`
already, so this adds no edge. `docs/api.md` and `doc.go` describe the flat API throughout and are updated in the same
phase as the examples.

`playground` builds only under `GOOS=js GOARCH=wasm` and is absent from `go list ./...`. `make lint` runs
`golangci-lint` over it separately at `Makefile:55`. That step has to run in every phase, otherwise a broken playground
import goes unnoticed until release.

## API breakage and what aliases can carry

The module is published on the Go module proxy, and a version the proxy has served is immutable there, so the flat
API is a published contract regardless of whether any dependent is known.
[`@v/list`](https://proxy.golang.org/actionlint.kjanat.dev/@v/list) lists every released version, and
[`@latest`](https://proxy.golang.org/actionlint.kjanat.dev/@latest) names the newest one together with the commit
hash it was cut from, for anyone pinning to an exact revision:

[![@latest version](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Factionlint.kjanat.dev%2F%40latest&query=%24.Version&label=%40latest)](https://proxy.golang.org/actionlint.kjanat.dev/@latest)
[![@latest commit](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Factionlint.kjanat.dev%2F%40latest&query=%24.Origin.Hash&label=commit)](https://proxy.golang.org/actionlint.kjanat.dev/@latest)

The module path itself resolves through a `go-import` meta element on the Pages site.

Against that, `docs/api.md:49-52` and `doc.go:17-21` both state that the version number belongs to the command line
tool, that the library does not follow semantic versioning, and that any patch bump may introduce breaking changes.
Whoever runs phase 11 decides which of the two facts governs.

If an alias layer is wanted, these are the Go rules, each verified in a throwaway module:

| kind                                | form                                          | works  | limit                                                                           |
| ----------------------------------- | --------------------------------------------- | ------ | ------------------------------------------------------------------------------- |
| type                                | `type Pos = ast.Pos`                          | yes    | full type identity, methods come along, `*Pos` and `*ast.Pos` are the same type |
| interface                           | `type Rule = rule.Rule`                       | yes    | same                                                                            |
| const                               | `const KindA = data.KindA`                    | yes    | stays a real constant, usable in const expressions and array sizes              |
| func                                | `var New = rule.NewRuleBase`                  | partly | the call compiles, but `go doc` renders `var New = rule.NewRuleBase`            |
| var holding a map, slice or pointer | `var PopularActions = actions.PopularActions` | partly | shares the underlying data until either side rebinds                            |
| var holding a scalar                | `var Count = pkg.Count`                       | no     | copied once at init, later writes on the other side are invisible               |
| generic func                        | `var F = pkg.F`                               | no     | `cannot use generic function pkg.F without instantiation`                       |
| method                              | none                                          | no     | there is no alias syntax for a method; methods travel with a type alias         |

The generic-function limit does not bite. `rg -n '^func [A-Z][A-Za-z0-9_]*\[|^type [A-Z][A-Za-z0-9_]*\[' *.go` returns
nothing, so the package has no exported generic declaration.

154 types and 60 consts, so 214 of the 276 top-level symbols bridge with no loss, and the 220 methods on exported
receivers come along with their type alias. The remaining 62 are 52 funcs and 10 vars. For the funcs, write a thin
wrapper function rather than `var F = pkg.F`. A wrapper stays a function in `go doc`, it can carry a doc comment, and
consumers cannot rebind it, which `reassign` at `.golangci.toml:34` already forbids for the current package. All 10
vars are maps
(`AllContexts`, `AllWebhookTypes`, `SpecialFunctionNames`, `BrandingColors`, `BrandingIcons`,
`OutdatedPopularActionSpecs`, `BuiltinFuncSignatures`, `BuiltinGlobalVariableTypes`, `PopularActions`,
`BuiltinUntrustedInputs`), so an alias shares the underlying map in all ten cases and only rebinding of the variable
itself is lost. None of the ten is rebound anywhere in the repository.

## Phases

Go refuses import cycles at compile time, so `go build ./...` is itself the cycle check. Any phase that introduces one
fails immediately.

| phase | content                                                                                                                                                                                                                                | endpoint                                                                                      |
| ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| 0     | this document                                                                                                                                                                                                                          | `dprint check`; no `.go` change                                                               |
| 1     | `internal` from `process.go`, `quotes.go` and the three action-pattern helpers; export 17 boundary symbols; update every `expr`, `rule`, `project` and root caller; move or rewrite the directly affected tests; make `SRCS` recursive | `make build SKIP_GO_GENERATE=true`, `make test`, `make lint SKIP_GO_GENERATE=true`            |
| 2     | `data` from `availability.go` and `all_webhooks.go`; move their tests; generators emit `package data`; `Makefile` and `//go:generate` updated                                                                                          | as phase 1, plus `go generate ./...` leaves a clean `git diff`                                |
| 3     | `glob` from `glob.go`; move `glob_test.go` and update the fuzz consumer                                                                                                                                                                | as phase 1, plus `make fuzz`                                                                  |
| 4     | `ast` from `ast.go`, `pass.go` and `nodeKindName`; export the nine boundary symbols; update all production and test consumers or add the corresponding transitional root aliases in this phase                                         | as phase 1, plus `go doc actionlint.kjanat.dev/ast Pos`                                       |
| 5     | `diag` from `error.go:1-228`; split `error_test.go` between `diag` and the root; update `cmd/actionlint-action`, `playground` and examples or add transitional root aliases                                                            | as phase 1, plus `go doc actionlint.kjanat.dev/diag Error`                                    |
| 6     | `expr` from the seven `expr_*.go` files; move the expression tests and update every production, fuzz and example consumer                                                                                                              | as phase 1, plus `go list -deps actionlint.kjanat.dev/expr` names neither `rule` nor the root |
| 7     | `project` from `config.go` and `project.go`; move their tests; rewrite root and rule tests that currently reach private project fields or helpers                                                                                      | as phase 1                                                                                    |
| 8     | `actions` from `action_metadata.go`, `popular_actions.go` and `reusable_workflow.go`; move their tests; rewrite rule tests against public cache methods                                                                                | as phase 1                                                                                    |
| 9     | `rule` from `rule.go` and the 21 `rule_*.go` files; move its tests; split `rule_required_actions_test.go` into internal helper tests, white-box rule tests and external parsing tests                                                  | as phase 1                                                                                    |
| 10    | finish remaining consumers and test-package conversions; exercise `cmd/actionlint-action`, `playground`, `fuzz`, `scripts/check-checks` and `scripts/generate-popular-actions`; remove transitional aliases if the API break is chosen | as phase 1, plus `GOOS=js GOARCH=wasm golangci-lint run ./playground` and `make fuzz`         |
| 11    | `docs/api.md`, `doc.go` and examples; finalize the root alias layer if compatibility is chosen                                                                                                                                         | `go test ./...` including the `Example*` functions                                            |

Every phase includes the consumers and tests that would otherwise stop compiling when that boundary moves. A
transitional root alias or wrapper is acceptable, but it must land with the move and be removed in phase 10 if phase 11
chooses the breaking API. Tests are not postponed wholesale: the owning phase moves or rewrites them.

Every new package needs a package doc comment as a repository convention. Nothing enforces it today.
`staticcheck.conf` lists `ST1000`, but golangci-lint does not read that file, and a package with no doc comment
produces zero issues under this repository's `.golangci.toml`.

Phase 1 is not additive: qualifying the moved helpers changes `expr_parser.go`, `expr_sema.go`, `expr_insecure.go`,
multiple `rule_*.go` files, `parse.go`, `linter.go` and `config.go`. It therefore needs coordination with branches that
touch those callers. Phases 2 and 3 have narrower generator, data, glob, test and fuzz call sites. Phases 4 to 10
relocate or split 41 of the 46 production files and can rewrite imports throughout the remaining production and test
files. Any open branch touching a root file becomes unmergeable without hand re-pathing, so those phases need a window
with no open feature branch.

## Corrections to issue #21

| the issue says                                                                        | measured                                                                                                                                                                                                |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| "428 exported symbols (149 types, 255 funcs, 10 vars, 10 consts)"                     | 154 types, 52 funcs, 60 consts, 10 vars, 223 methods. The listed numbers also sum to 424                                                                                                                |
| two files have no exported symbols                                                    | three: `process.go`, `quotes.go`, `doc.go`                                                                                                                                                              |
| "`actionlint.kjanat.dev` has never been tagged", "API breakage: None"                 | 63 tags, five versions on the module proxy, vanity import live                                                                                                                                          |
| "27 commits touching root `.go` files in the last six months, across 10 unique files" | 14 commits over 10 unique root files. 27 is the count for any `.go` file in the module. The fetched `upstream/main` also stops at 2026-04-19, so the window holds about two months of upstream activity |
| the five-package layout                                                               | contains three import cycles, none of them named in the issue                                                                                                                                           |

65 of the 82 root files differ from `upstream/main` at `011a6d15`: 17 identical, 57 changed, 8 present only in the fork
(`rule_parallel_steps.go`, `rule_action_test.go`, `rule_shell_name_test.go`, the three policy-rule files and the two
new policy test files).

## Reproducing the measurements

| claim                                                  | command                                                                                                                                             | falsified by                                                                                                                                                                            |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| the `Rule` interface names `*Config`                   | `rg -n 'SetConfig\|Config\(\) \*Config' rule.go`                                                                                                    | no hit on lines 115-116                                                                                                                                                                 |
| `expr` uses `availability.go`                          | `rg -n 'SpecialFunctionNames' availability.go expr_sema.go`                                                                                         | no hit in `expr_sema.go`                                                                                                                                                                |
| the metadata caches use `Project`                      | `rg -n '\*Project\|RootDir\(\)' action_metadata.go reusable_workflow.go`                                                                            | no hit                                                                                                                                                                                  |
| `reusable_workflow.go` uses `parse.go`                 | `rg -n 'nodeKindName' parse.go reusable_workflow.go`                                                                                                | no hit in `reusable_workflow.go`                                                                                                                                                        |
| `{ErrorFormatter, command, linter}` is a cycle         | `rg -n 'getCommandVersion' error.go command.go` and `rg -n 'RegisterRule' error.go linter.go`                                                       | either returns nothing                                                                                                                                                                  |
| `{expr_insecure, expr_sema}` is a cycle                | `rg -n 'errorfAtExpr\|UntrustedInputChecker' expr_sema.go expr_insecure.go`                                                                         | no use of `errorfAtExpr` in `expr_insecure.go`                                                                                                                                          |
| `{config, rule_required_actions}` is a cycle           | `rg -n 'invalidActionPattern\|RequiredActions\(' config.go rule_required_actions.go`                                                                | no hit in either direction                                                                                                                                                              |
| `expr` touches neither `Pos` nor `Error`               | `rg -n '\bPos\b\|\bError\b' expr.go expr_ast.go expr_lexer.go expr_parser.go expr_sema.go expr_type.go expr_insecure.go`                            | 11 hits, all from `text/scanner` (`scan.Pos()`, `scan.Error`) and `(*ExprError).Error()` at `expr.go:18,23`. A twelfth hit, or one naming `ast.Pos` or the linter `Error`, falsifies it |
| the AST and the expression tree are independent        | `rg -n 'ExprNode\|ExprType\|ExprError' ast.go pass.go`                                                                                              | any hit                                                                                                                                                                                 |
| `compareErrors` and `equalsErrors` are production code | `rg -n 'compareErrors\|equalsErrors' --type go`                                                                                                     | hits only in `_test.go` files                                                                                                                                                           |
| `byteOffsetAtColumn` is used inside `ast.go`           | `rg -n 'byteOffsetAtColumn' --type go`                                                                                                              | no hit at `ast.go:93`                                                                                                                                                                   |
| the module is published                                | `curl -sS 'https://proxy.golang.org/actionlint.kjanat.dev/@v/list'`                                                                                 | empty output or 404                                                                                                                                                                     |
| the vanity import is live                              | `curl -sSI 'https://actionlint.kjanat.dev/?go-get=1'`                                                                                               | a status other than 200                                                                                                                                                                 |
| 65 of 82 root files differ from upstream               | `for f in *.go; do git cat-file -e upstream/main:"$f" 2>/dev/null \|\| echo "$f new"; done` and `git diff --stat upstream/main HEAD -- '*.go'`      | a different count                                                                                                                                                                       |
| 14 upstream commits touch root `.go` in six months     | `git log --oneline --since="$(date -d '6 months ago' +%Y-%m-%d)" upstream/main -- '*.go' ':!cmd' ':!scripts' ':!playground' ':!fuzz'`               | a different count                                                                                                                                                                       |
| no exported generic declaration                        | `rg -n '^func [A-Z][A-Za-z0-9_]*\[\|^type [A-Z][A-Za-z0-9_]*\[' *.go`                                                                               | any hit                                                                                                                                                                                 |
| the alias table                                        | a throwaway module with `type T = pkg.T`, `const C = pkg.C`, `var F = pkg.F`, `var V = pkg.V`, a `[C]int` array and a function that rebinds `pkg.V` | any row behaving differently                                                                                                                                                            |
| `ST1000` does not fire                                 | a throwaway module with this repository's `.golangci.toml`, `staticcheck.conf` and a package without a doc comment, then `golangci-lint run ./...`  | any issue reported                                                                                                                                                                      |
| the layout is acyclic                                  | after phase 10, `go build ./...` succeeds and `go list -deps actionlint.kjanat.dev/expr` names neither `rule` nor the root                          | `import cycle not allowed`, or an unexpected dependency                                                                                                                                 |

Once the move is done the Go compiler enforces every cycle claim in this document. Before the move, the seven rows at
the top of the table carry the proof, and each is one `rg` away.

## Open questions

1. `data` or `gha` as the name for the generated-table package. `data` says nothing, but `availability.go` and
   `all_webhooks.go` are both plain GitHub Actions reference data.
2. `diag` or `errs` as the name for the diagnostic package.
3. Alias layer in the root, yes or no. 214 of the 276 top-level symbols bridge without loss, 52 funcs need wrappers and
   10 vars lose only rebinding. Roughly 300 lines of root code plus doc comments. The alternative is a `v2` module path
   and taking the break, which `docs/api.md:49-52` already warns consumers about.
4. Whether `Rule` keeps `SetConfig(*project.Config)` or takes a local interface instead. Rules read exactly three
   members of `Config`: `Config.SelfHostedRunner` in `rule_runner_label.go`, `Config.ConfigVariables` in
   `rule_expression.go` and `Config.RequiredActions()` in `rule_required_actions.go`. A local interface would leave
   `rule` independent of `project`, at the cost of a harder break in the published `Rule` interface.
