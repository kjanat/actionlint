# v1 (Go) inventory — what to preserve, what it did well, what surprised

Read from a plain extract of `f829a4ea` (`internal/` ≈ 95k source lines,
127k test lines). This is a capability floor and a technique list, not a
port plan (ADR-0001). `path:line` refers to that commit.

Rule-id mapping: v1 ids are `rule/code`. V2 ids are `area/name`
(ADR-0015). Where a v1 finding has no v2 home yet it is marked **→ new**.

## 1. Expressions, typing, conditions

`internal/expression` 5.5k + `context/` 0.5k + `workflow/expressions` 2.1k

- `semantics/` 0.6k + `workflow/matrix` 1.3k + `conformance/` 0.2k source.
  11.9k tests. Plus 1.4k TypeScript in `expression/probe/` that runs real
  `workflow_dispatch` jobs.

### Preserve

- Lexer: idents `[A-Za-z_][A-Za-z0-9_-]*`, `''`-escaped single quotes
  (`"` → hint "only single quotes"), ints, lowercase `0x` hex only, floats
  with fraction/exponent, `-` always starts a literal (no binary minus),
  alnum right after a number is an error (`017`, `1_000`). `}}` is END.
  `expr_lexer.go:311,342,354,527,541`
- Parser: `||` > `&&` > compare > `!` > postfix (`.p`, `.*`, `[i]`) >
  primary. Calls only on identifiers. `null/true/false` case-sensitive.
  Names lowercased at parse. Int via `ParseInt(v,0,32)`. Depth 50 with
  same-kind chains counted once. Trailing tokens reported. Compare ops
  parse **right**-recursively (`a == b == c` → `a == (b == c)`).
  `expr_parser.go:85,158,302,364,391`
- `${{ insert }}` recognised (`IsMappingInsertion`), full-consumption
  parse entry points. `expr_parser.go:437,452`
- Condition compilation: bare `if:` gets `}}` appended. Blank →
  `success()`. Single string-literal expression unwrapped and re-parsed.
  `${{ }}` with surrounding text → "always true". `condition.go:17`,
  `semantics/condition/check.go:25` → v2 ADR-0016 "if compilation"
- Type lattice any/null/number/bool/string/object{strict,loose,mapped}/
  array. `Assignable` asymmetric (string accepts number, bool accepts
  everything). `Merge` conflict → any. `expr_type.go:89`
- Builtin signatures with overloads (`contains`×2, `join`×4, `format`×2,
  `case` variadic, `hashFiles` variadic), `IsConstFunc`, 255-arg cap.
  `expr_sema.go:139,1194`
- Builtin contexts incl. undocumented `github.artifact_cache_size_limit`,
  `github.output/state/step_summary`, `repository_visibility`,
  `runner.environment`. Builtin secrets `github_token`,
  `actions_step_debug`, `actions_runner_debug`. `expr_sema.go:286`
- Context/function availability per workflow key. `success()/failure()`
  accept job-name args in `jobs.<id>.if`. `expr_sema.go:647,1199`
- Suggestions: single-edit typo, `github_sha`→`github.sha`, bare
  `foo`→`inputs.foo`, `always`→`always()`. `expr_sema.go:688`,
  `event.go:496` → v2: fixes on `expression/unknown-*`
- `.*` object filter on objects and arrays. Strict object with no object
  member rejected. `expr_sema.go:907`
- Index: array needs number, object needs string. Literal string index
  also runs `vars`/`secrets` name checks. `expr_sema.go:953`
- `vars.*` naming (`[a-z0-9_]`, no `github_` prefix) + config allowlist
  (nil = any, empty = none). `expr_sema.go:832`
- `format()` placeholder parser incl. `{{` escape and odd/even `}` runs.
  Unused args and missing placeholders both reported.
  `expr_sema.go:37,1078`
- `fromJSON('literal')` → structural type. JSON syntax error with offset.
  Case-insensitive member collision. `expr_sema.go:1098`,
  `expr_json.go:17` → v2 `expression/json-key-collision`
- `case()`: odd arity, bool predicate, dead-branch `skipEvaluation`.
  `expr_sema.go:1118`
- Comparison validity matrix (`==` object only vs object/null/any. `<`
  family forbids null/bool/object/array). `expr_sema.go:1241`
- `&&`/`||` typing with truthiness narrowing and constant-folded
  dead-branch suppression. `expr_sema.go:1313`
- Untrusted-input tree: `*` array elem, `**` any prop, object-filter
  fan-out, `toJSON(container)` reports all leaf paths. Safe calls
  `contains/startsWith/endsWith` and boolean positions suppress.
  `expr_insecure.go:99,454,524` → v2 `script/untrusted-input`
- Secret interpolation finding (`secrets.*`, `github.token`,
  `github['token']`), suppressed under overlapping syntax error.
  `secret_interpolation.go:5`, `expr_sema.go:1411` → **new**
  `script/secret-interpolation`
- Script-interpolation proof model: positions Unknown/ShellWord/
  ShellArgument. `SafeScriptLiteral` `[A-Za-z0-9_./:-]`. `SafeScriptWord`
  excludes shell keywords, `elif`, IO numbers. Status enums and fixed
  `github.*`/`runner.*` fields proven safe. Echo/printf arg positions via
  masking `${{ }}` with `0`s and parsing with mvdan/sh (bash/sh, direct
  `{0}` only). Matrix values proven safe per path incl. `[*]`. Env literal
  proofs. `script_interpolation.go:71,150,229`,
  `workflow/expressions/script_positions.go:92`, `script_values.go:207,226`
  → **new** `script/unknown-script-value`. Needs ADR-0016 literal unions
- Unsound ternary `a && b || c` with definitely-falsy `b`.
  `expr_ternary.go:5` → v2 `expression/ternary-idiom`
- Condition-only checks `string-conditions`, `mixed-type-comparisons`,
  `case-insensitive-conditions` (also on contains/startsWith/endsWith).
  `expr_condition_checks.go:72`, `expr_sema.go:1069,1404` → v2
  `expression/string-truthiness`, `cross-type-compare`,
  `case-insensitive-gate`
- Constant-fold subset: literals, `!`, `&&`, `||`, compare, `case`,
  `toJSON(null/bool/safe string)`, `join`, `fromJSON` (no collisions),
  `contains/startsWith/endsWith`, `format`. `condition_constant.go:30`
- LSP completion for dot/bracket access, never inside a string.
  `completion.go:17`
- Workflow field typing: `if` bool-assignable. Template values not
  object/array/null. Static fields reject expressions. `workflow_call`
  input default vs declared type. Reusable-workflow input assignability.
  `queue: max` + `cancel-in-progress`. `workflow/expressions/fields.go:
  237,300,399,404`, `workflow.go:73`
- Context construction: step outputs from local `action.yml` / popular
  catalog, `github-script` → loose, `SkipOutputs`. `needs` = direct deps
  only. Call + dispatch inputs merged. `secrets` mapped when non-call
  triggers exist. `dependency_types.go:11,71`, `expr_sema.go:541`,
  `workflow.go:94`
- Matrix type: merge reachable values, loose when open, any when includes
  incomplete. Diagnostics: duplicate value, exclude without axes, empty
  exclude, unknown exclude axis, unmatched exclude value,
  `mixed-type-matrix-filters`. `matrix.go:84`, `matrix/check.go:17` → v2
  `matrix/*`, `matrix/filter-loose-equality`
- Conformance: 4 pinned sha256 zips (languageservices, runner,
  schemastore, yaml-test-suite). Fixed counts 1228/24/67/402.
  `differences.json` baseline + `assessments.json` categorised rationale
  (dependency-gap 58, extra-lint 42, actionlint-gap 8, adapter-limit 2,
  contract-difference 2, reference-gap 2). `conformance/sources.go:24`,
  `conformance_test.go:117`, `testdata/assessments.json:1` → v2
  `fixtures/conformance/` (ADR-0017) must absorb these too

### Done well

- `ordinalIgnoreCaseKey`: uppercases but refuses non-ASCII→ASCII (ı→I,
  ſ→S), matching .NET `OrdinalIgnoreCase`. `expr_json.go:9`
- String ordering by UTF-16 code units of the folded strings (runner
  `CompareOrdinal`). `condition_constant.go:210`
- Negative zero as `Copysign(0,-1)`. `constantPredicateString` refuses
  `-0`, `|v| ≥ 1e15`, non-integers so no G15 formatting is assumed.
  `condition_constant.go:37,196`
- `ToNumber` mirrors runner: trim, `Infinity`, YAML core-schema decimal,
  `0x`/`0o` via signed-32 wrap, else NaN. `0X` → NaN. `expr_number.go:11`
- Composite-vs-composite compare unknown (identity), composite-vs-primitive
  known (NaN). `condition_constant.go:214`
- `toJSON` folds only strings needing no escaping in either JSON engine.
  `join` refuses number serialisation. `condition_constant.go:99,230`
- `case()` truthiness: reachable branches must agree. Known-false
  predicates skipped. `expr_ternary.go:55`
- EventVariant guard narrowing on `github.event_name`/`github.event.action`
  `==`/`!=` per branch. Strict payload without `action` proves absent.
  Zero variants → any. Copy-on-write var tables. `event_variants.go:32,61`,
  `expr_sema.go:476`
- `availability.gen.go` generated from github/docs `contexts.md` "Context
  availability" table via goldmark: strips Liquid, stops at `{% else %}`,
  dedups identical rows, hard-codes runner-only keys
  (`cancel-timeout-minutes`, `snapshot`, `snapshot.if`) from
  `workflow-v1.0.json`. `context/generate/main.go:91,241` → v2 reads the
  `context` arrays directly (ADR-0014), no markdown scraping
- Node spans start-token→last-consumed. Parens re-span inner node. Error
  offset clamped on synthetic `}}`. `expr_parser.go:76`, `condition.go:24`
- Untrusted checker visits index before operand so `foo[a.b].bar` works.
  Cursor compaction. `expr_sema.go:954`, `expr_insecure.go:244`
- Composite-action script pass snapshots/restores the whole checker and
  keeps only script findings. `workflow/expressions/composite.go:13`
- Conformance expression adapter checks symbols/arity only (upstream
  parser doesn't type). `conformance_test.go:382`
- Probe corpus: 85 cases run as real jobs, dual mode runner vs
  languageservices, corpus sha pinned. `probe/cases.ts:20`,
  `probe/probe.test.ts:24` → v2: fold into ADR-0017 oracles
- Matrix include/exclude with `matchProof {never, uncertain, always}` +
  `Subset`. Hex index keys in filters. `matrix/resolution.go:709,928`

### Surprising

- No 21000-char expression length limit anywhere (runner has one). Only
  depth 50. `expr_parser.go:380`
- TODO "Is String of numbers coerced into number?" on
  `NumberType.Assignable`. `expr_type.go:90`
- TODO: generate untrusted inputs from CodeQL `ExpressionInjection.ql`.
  `expr_insecure.go:97` → v2 should do exactly that
- Comment "This document is actually wrong" re `if:` without `${{ }}`.
  `workflow/expressions/fields.go:361`
- Hard-coded 7 payload text paths typed string and 11 "identity" refs for
  case-insensitivity. `expr_condition_checks.go:21,40` → v2: from
  `webhooks.json` + a vendored attacker-namable list
- `github.event…*` object filter is `array<any>` unconditionally.
  `expr_sema.go:929`
- `skipEvaluation` suppresses only `fromJSON`/`format` checks in dead
  branches. `expr_sema.go:1065`
- JSON collision walker: depth cap 10000, silently nil on malformed or
  trailing JSON. `expr_json.go:35,75`
- `hashFiles` treated as a safe sink for secrets.
  `secret_interpolation.go:30`
- Only `github` is an untrusted root. `expr_insecure.go:259`
- Composite scripts drop all non-script findings. `semantics.go:171`
- Workflow-level checker emits rule `expression` with empty code for every
  shape error. `workflow/expressions/checker.go:78`
- `github.event.inputs.*` forced to string while `inputs.*` keeps declared
  type. `expr_sema.go:559` (correct per runner)
- A step `id` containing an expression makes the whole `steps` context
  loose. `workflow.go:249`
- Shell detection only bash/sh with direct `{0}`. Pwsh/cmd → every
  non-constant interpolation flagged. `script_positions.go:44`,
  `script_interpolation.go:131`
- Documented contract differences vs upstream: empty expression is null
  upstream. `NaN`/`Infinity` are undefined variables here. Runner
  whitespace is `Char.IsWhiteSpace` (U+00A0) vs 4 ASCII chars.
  `conformance/testdata/assessments.json:36,57`, `differences.json:6`
- Glob: `.`/`..` path prefixes rejected. `\` escape set differs ref vs
  path. `semantics/glob/glob.go:243`
- Matrix `looseEqual` reuses expression `ToNumber` + `EqualFold`.
  `matrix/resolution.go:730`

### v1 ids in this area

- `expression-syntax/invalid-syntax`
- `expression-reference/{unknown-variable, unknown-property,
  unknown-function, invalid-variable-name, unknown-config-variable,
  unknown-secret}`
- `context-availability/{unavailable-context, unavailable-function}`
- `expression-call/{missing-call, argument-count, format-placeholder,
  invalid-json, json-member-collision}`
- `expression-type/{argument-type, operand-type}`
- `event-payload/unknown-property`
- `script-injection/{untrusted-interpolation, secret-interpolation}`
- `script-interpolation/unknown-script-value`
- `unsound-ternary/falsy-middle-operand`
- `string-conditions`, `mixed-type-comparisons`,
  `case-insensitive-conditions`, `expression` (no code), `if-cond`,
  `matrix`, `mixed-type-matrix-filters`, `env-var`, `id`, `job-needs`,
  `permissions`

## 2. Document model, parsing, identity, workflow/action semantics

33.6k source / 36.7k test. `internal/workflow` alone 23.5k / 19.3k. Note: `internal/documentparse/internal/frozen` does not exist. "frozen" is vocabulary only. The generator lives at `documentparse/internal/generate/main.go`.

### Preserve

- Source identity `Reference{Document, Revision, Node, Address}`: Document = canonical physical path, Revision = lowercase SHA-256 of bytes, Node = stable per-declaration hash, Address = JSONPath-ish display (`jobs["x"].steps[0].parallel[1]`). `source/source.go:5`, `source/revision.go:19` → ADR-0003 `SourceRef` + `Revision`. **adopt `Address`** for messages
- Two coordinate models (`yaml`: BOM stripped, CR/LF/CRLF/NEL/LS/PS breaks. `raw-lf`). Capture store refuses mixing per document. `source/coordinates.go:10`, `source/capture/store.go:339`
- Capture `Store`: one read per canonical document, overlays (in-memory/stdin), alias table, sticky quarantine on contradictory evidence, `VerifyHandle` rejects foreign/stale/quarantined snapshots. `source/capture/store.go:299-402` → salsa inputs + `DocumentId` do this structurally (ADR-0003/0004)
- Step node identity: `owner\0stepID` or `owner\0anonymous\0digest(fingerprint)` + occurrence counter, hashed with document id — survives line moves, distinguishes duplicate anonymous steps. Fingerprint = length-prefixed structural values, sorted keys, no positions/quoting, recursive into `parallel`. `sourceid/identity.go:37`, `workflow/facts/step_identity.go:15` → **adopt** as `NodeId` for suppressions and LSP stability
- Invocation graph `Entry{workflow|action}` → `Call{parent, site, depth, Facts, Outcome}` → `Script{owner, decl, kind run|action-input, input name}`. Ids = SHA-256 over `actionlint:invocation:v1\0domain\0len:field…`. Call id = hash(parent id, site doc+node) so one step via two paths is two occurrences. `workflow/invocation/graph.go:126-176` → ADR-0006 `Occurrence` path hash is equivalent. Keep the domain-separated hashing
- Closed outcome set: `expanded | opaque(dockerfile-only, non-composite) | unavailable(non-local, not-fetched, lookup-disabled, placement-uncertain) | unresolved(invalid-call, missing-manifest, read-failed, parse-failed, mapping-failed) | dynamic(expression) | cycle | depth-limit | interrupted(canceled, operational-failure)`, reasons validated per kind. `invocation/outcome.go:122` → **adopt verbatim** as the `Unresolved` enum
- Budget per entry MaxDepth 10, MaxCalls 10000, MaxScripts 10000. First exhaustion recorded as `Stop{Parent,Site,Limit,Max}`. Graph flagged `Incomplete`. `graph.go:100,455` → ADR-0006 default 16 was arbitrary. Use these
- Cycle = nearest open ancestor whose expanded target is the same doc+node. `graph.go:567`
- Call Facts: step position, uses, condition `enabled|skipped|runtime-dependent`, AfterFailure, ContinueOnError, Background, shell/path/checkout/lookup env-unknown flags. `invocation/context.go:21`
- Composite expansion only when `runs.using == composite` (case-insensitive). Reusable workflows via `calls.Cache` (inputs/secrets/outputs lower-cased, job permissions). `composition/invocations.go:135`, `calls/reusable_workflow.go:47`
- Reusable-workflow checks: required/undefined inputs and secrets (unless `secrets: inherit`), callee job permission vs caller grant. `calls/check.go:87` → `call/*`
- Public `CallGraph` immutable, validated, paged (256). `Input.ValidateInvocations` cross-checks every script occurrence. `analyzer/call_graph.go:149`
- `diagnosticpolicy`: inline suppressions, configured `ignores:` (rule/file/job/step/uses), severity levels, regex ignores, dedupe, path relativisation, sort. `diagnosticpolicy/policy.go:30-154`
- Inline grammar (exact): `# actionlint:ignore RULE[,RULE] -- reason` same line. `# actionlint:ignore-next-line RULE -- reason` own line. Reason mandatory after `--`. Unknown rule names rejected. `suppression/inline.go:44-63` → ADR-0015 chose `# actionlint-ignore id: reason`. **Switch to v1's grammar**: `actionlint:ignore` + ` -- reason` is already in users' files and the `--` separator is unambiguous against rule ids
- `policy.disallow-suppressions` per rule: `allowed | report-suppression | report-all`. `diagnosticpolicy/configured.go:59` → **adopt** as `[suppressions] disallow = [...]`
- Foreign (action.yml) findings filtered by the *declaring* manifest's own inline comments and its own config. `diagnosticpolicy/inline.go:57`
- `Collect` dedupes by (project, path, rule, start, deep-equal). Suppresses `action` findings whose manifest already produced `syntax-check`. `collect.go:23,43`
- `workspace` discovers `.github/workflows/*.yml|yaml` + `action.yml|yaml` repo-wide, skipping `.git node_modules vendor .venv venv .cache .worktrees testdata` and nested repos. Honours `.gitignore` via git. `workspace/discovery.go:15`, `projectpath/actions.go:317`
- Project root = nearest `.git` (file or dir), cached. `workspace/project.go:28`
- Git index snapshot (`ls-files --stage -z` + intent-to-add) cached once per session for executable-bit checks. `workspace/git_index.go:20` → `script/executable-bit` needs it
- Runner knowledge per job: Platform (Any/MacOrLinux/Windows), default Shell, CaseInsensitive, ShIsDash, Ephemeral, PathsKnown. Pluggable providers (builtin/snapshot/external), evidence hashed. `workflow/runner/knowledge.go:46` → `runner_facts` provider, same fields
- Hosted label table + OS-compat bitmask conflict detection. `runner/labels.go:47,228` → `runner/label-conflict`
- `workflow_run` producer-name check against repo inventory with GitHub glob grammar incl. `!`. `names/check.go:28`, `names/pattern.go:63` → `events/unknown-workflow-run-source` **new**
- Shell inference step > job > workflow > runner default. Container → `sh`. Remote fallback `bash`. `{0}` custom templates skipped. `execution/shell.go:17`
- Action manifest model: inputs/outputs lower-cased, `runs` union (node/docker/composite/plugin), branding, `deprecationMessage` normalisation. Schema validation mirrors TemplateReader coercion, 100-depth cap, anchors/aliases rejected. File-exists checks for `main`/`Dockerfile`. `action/metadata.go:249`, `action/schema.go:49-126`, `action/validation/check.go:45`
- Local-action resolver tracks checkout placement per job, case-insensitive repo lookups, `placement-uncertain` when checkout dir unknown. `action/local/resolver.go:241`
- Modelled: `cache-mode` (none/read/write/write-only), parallel/background/wait/cancel steps. `workflow/cache_mode.go`, `workflow/checks/parallel.go:30`
- Public `analysis` API: `Analyze` (in-memory), `Files`, `Repository`. `analysis/analysis.go:97`

### Done well

- Owned `syntax.Node` graph round-trips to the forked go-yaml node graph with per-node identity maps. Parser Kind/Style bit tables validated both ways. `yaml/codec/bridge.go:118` → irrelevant with tree-sitter (ADR-0002), but the **round-trip test** idea stays
- Non-mutating alias admission: walks the physical graph with a dialect-supplied `follow(parent,index)` predicate, counting constructions/aliases without building values. `codec/admit.go:28,41` → ADR-0002 open item "alias budget lives in semantics" — this is the design
- Session cursor aggregates per-load alias deltas into one document-wide budget so split decoding matches a plain `Unmarshal`. Fatal errors sticky across the session. `codec/cursor.go:118-291`
- Graph validation: content cycles must go through an alias. Alias → concrete target only. Mapping pair parity. `codec/graph.go:19`
- Parser pipeline: index scalar sources → flow-null check → alias inspection (recursive/undeclared/`+`-in-name/unused) → admission → detached projection where accepted alias slots are replaced by copies, `aliases` map keeps provenance. `parser/parse.go:2106`, `alias_projection.go:26` → architecture.md `at`/`defined_at`
- Immutable split: `declaration.Publish` deep-clones (generated `clone.gen.go`), builds a Skeleton, computes `value.Value`s. `Bind(identity)` yields `Bound` with opaque views (`_ [0]func()` forbids compare/copy). `declaration/record.go:59-139` → salsa tracked structs give this for free
- Views + clones generated from the Go type closure with `go/types`, no reflection. `documentparse/generate.go:5`
- `SourceSnapshots` binds one declaration revision per document. Conflicting revision/kind quarantines. `documentparse/snapshot.go:101-148`
- Scalar source maps decoded lazily. Collection indent recovered from physical lines for anchored/tagged mappings. `parser/parse_scalar_source.go:18-100`
- Comment attachment indexed before projection so suppression positions survive aliasing. `parser/scalar_comments.go:23`
- Strict position → byte offset per coordinate model, no clamping. `source/position/range.go:17`
- Composite traversal copies the analyzer frame per call so a child cannot replace caller source/config. `Begin → Resolve → Close` with deferred interrupt. `composition/composite_scripts.go:24-57`
- Resolver `ForkComposite` isolates checkout/env per child, one open child at a time. `action/local/scope.go:70`
- Checkout placement as immutable linked chain. Executable-bit resolves through placements + case-folded git index. `execution/checkout.go:17`, `execution/index.go:37`
- Condition analysis proves `if:` never runs across status sets (jobs without `needs` exclude failure). `execution/condition.go:14` → `job/unreachable-if`
- Runner facts: shell (`bash`/`pwsh`), case-insensitivity (Windows or all-mac), `ShIsDash` (all-ubuntu), `PathsKnown`. Multiple candidates joined conservatively. Matrix-expanded selectors capped 256 → `Incomplete`. `runner/knowledge.go:322-345`
- Builtin dataset identity = hash of label tables → reproducible evidence. `runner/provider.go:16`
- Names index: `SamePath` handles symlinks, case-insensitive FS probing (darwin pathconf / directory-entry evidence). `names/index.go:35`, `names/case_sensitivity.go:16`
- Action metadata `Scalar()` mirrors runner TemplateReader coercion. Physical-tree scan rejects anchors before admission. `action/schema.go:39,84` → `action/anchor-not-supported`
- Manifest decoder lower-cases keys, reports duplicates with marks, keeps partial declarations on error. `action/decode.go:15,106`
- Reusable-workflow metadata decoded twice (codec for inputs/secrets/outputs, full parser for permissions/cache/jobs). Parse errors memoised per caller. `calls/metadata_decode.go:18`, `reusable_workflow.go:235`

### Surprising

- **`TestNestedHookKeepsAliasBudgetFromCapturedOccurrence` is at `codec/session_test.go:151` and passes at the pinned go-yaml fork** (`…20261027160845-bf5e3e7bc566`). It depends on fork-only `LoadWithAliasContext` + `WithOccurrenceFunc` and on upstream counting every node under an alias crossing as an alias (ratio 0.99 trips at 1300/1301). The name is a relic ("retained for exact -run gates"). Earlier session notes calling it "pre-existing failing" were measuring against a different fork pin.
- Upstream alias policy is a ratio heuristic (≤100 aliases or ≤1000 constructions always OK), not a hard count — the "budget" was inherited. `codec/cursor.go:57` → ADR-0002 must define its own
- Merge key `<<` rejected: "GitHub Actions does not support YAML merge key". `parser/parse.go:457` → runny should confirm. Then `syntax/merge-key` (correctness)
- Anchor/alias names containing `+` rejected because GitHub's reader rejects them. `alias_projection.go:114`
- Compact flow null `{key:}` flagged because GitHub's reader absorbs the colon into the key. `parse_yaml_compatibility.go:14` → `syntax/compact-flow-null`
- Action metadata rejects anchors, yet composite steps are still cloned with alias resolution for script analysis — two alias policies for one file. `action/schema.go:84`, `documentparse/action_steps.go:42`
- Recursive alias left unexpanded to avoid stack overflow (#610). `alias_projection.go:134`
- `XXX: Is the permission scope case insensitive?` / `XXX: Is the key case-insensitive?` (services). `parse.go:912,1396` → runny can answer both
- `delayedSprintf` "reduces allocations by 4.94%". `parse.go:100`
- Branding icon list hand-copied, TODO "generate from Feather Icons JSON". `validation/branding.go:40` → `xtask vendor-github` adds Feather
- TODO: no validation for `workflow_dispatch` input type `environment` defaults. `checks/events.go:259`
- Label tables hard-code future runners (ubuntu-26.04, macos-26, xcode-27, windows-2025-vs2026) as data. `runner/labels.go:47` → ADR-0014 §3 vendors instead
- "Alias may duplicate errors" → final `CompactFunc` dedupe is a band-aid. `policy.go:149`
- Severity override mutates `Finding.Severity` in place inside `DeleteFunc`. `policy.go:123`
- Action discovery hard-requires the `git` binary inside a repo. `projectpath/actions.go:65` → v2: `gix` or a pure-Rust ignore walker, no subprocess
- `script-input` catalogue hard-codes `actions/github-script` `script` as JavaScript. `composition/analysis.go:191` → an extension manifest field
- Document `Facts()` for actions wraps composite steps in a synthetic workflow with one job keyed `""`. `documentparse/document.go:72` → ADR-0005: don't
- Legacy issue refs retained as behaviour anchors: #104, #151, #164, #333, #610.

## 3. Application, config, CLI, LSP, output, Action, catalogs

30.3k source / 43.3k test. `application` alone is 1.8k source / 11.2k test.

### Preserve

- Subcommands: `check`, `fix` (`--apply`, `--include-suggested`), `pre-commit` (hidden), `config {init,path,show --origin,validate,migrate --write|--check}`, `rules [rule]`, `plugin {inspect,search,versions,install,list,update,remove,sync}`, `doctor`, `completion <shell>`, `version`, `lsp`. `cli/root.go:69-213`, `cli/plugin.go:30-57` → v2 ADR-0011 gains `config`, `rules`, `doctor`, `completion`, `fix`. `plugin` → `extension`
- `check` flags: `--json`, `--json-pretty`, `-q/--quiet`, `--hyperlinks {auto,always,never}`, `--config`, `--no-config`, `--plugin URI`×n, `--strict`, `--experimental`, `--stdin-filename`, `--plugin-lock`, `--runner-target` (default `github`), `--runner-provider`, `--runner-snapshot FILE`, `--runner-scope`, `--ignore-regex`, `-o/--output-format {text,pretty,oneline,json,jsonl,sarif,github}`, `--diagnostic-width` 40–400, `-f/--template`, `--template-file`, `--output-file` (`-`), `-y/--yes`, `--summary`, `--oneline`, `-v/--verbose`, `--debug`, `--log-level`, `--color`, `--no-color`, `--help-legacy`. `cli/root.go:216-320` → v2 drops `--ruff/--shellcheck/--pyflakes/--allow-config-processes` (ADR-0008), keeps the rest
- Exit codes 0/1/2/3, 3 = could not complete, ctx cancel → 3. `render/format.go:18-27` → v2 ADR-0011 uses 3 for "nothing to analyse": **add exit 4** for internal failure so the two are distinct
- Single-dash legacy flags normalised. `ACTIONLINT_*` env mirror for every flag. Honours `NO_COLOR`, `NO_HYPERLINKS`, `FORCE_HYPERLINKS`, `GITHUB_ACTIONS`. `cli/args.go:30-72`, `cli/env.go:54-179`
- Output: text (`path:line:col: msg [rule]` + snippet/caret), pretty (rustc-style), oneline, json (`CheckResult` `schema_version: 1`), jsonl, sarif, github (`::error file=,line=,endLine=,col=,endColumn=,title=::`), Go template. `render/result.go:48-172`, `render/template.go:14-105`
- JSON mode wraps stderr log lines as `{"log":…}` and errors as `{"error","exit_code"}`. `cli/output.go:54-108`
- Config discovery `.github/{actionlint.yaml,yml,json,toml}` nearest repo root. `--config` skips project config. `config/config.go:700-718` → v2 ADR-0010: root `actionlint.toml`, **also** `.github/actionlint.toml`. `workflow/legacy-config-file` for the yaml names
- Config keys: `extends`, `plugins`, `files{includes,excludes}`, `lint{enabled,rules{preset,correctness,suspicious,security,policy,external,plugins,nursery,disable},message-ignores}`, `overrides[]{includes,excludes,lint,tools,local-actions}`, `ignores[]{rule,file,uses,job,step,reason}`, `local-actions`. `config/config.go:352-374`, `config/lint.go:14-39`, `config/ignores.go:16-26` → v2 ADR-0015 `[rules]`/`[[overrides]]`. **adopt** `ignores[]` with `uses/job/step` selectors + mandatory `reason`, and `local-actions` (this repo's own config needs it for CI-built actions)
- Legacy keys migrated: `policy{…}`, `self-hosted-runner.labels`, `config-variables`, `config-secrets`, `assume-default-permissions`, `paths{glob:{ignore}}`. `config/migration.go:17` → v2 `config migrate` from both v1 shapes
- Rule levels `default|off|on|info|warn|error`. Presets `recommended|all|none`. Per-rule `{level, options}`. `config/rules/settings.go:13-113` → identical to ADR-0015
- LSP: stdio JSON-RPC, utf-16, incremental sync, `completion` (triggers `. [ '`), `codeAction`, versioned `publishDiagnostics`, `window/logMessage`. `langserver/server.go:252-432`
- GitHub Action inputs: `version, version-file, install-only, plugin-cache, files, format, ignore, config-file, config, shellcheck, ruff, pyflakes, add-actionlint-to-path, working-directory, output-file, fail-on-error, annotations {auto,true,false}, summary, summary-title, sarif, review, token`. Outputs `version, exit-code, result, problems-found, problem-count, output, output-file, result-file, report-sarif`. `runs: node24`. `action.yml:7-129`
- Action = JS launcher (binary download, SHA-256 verify, `@actions/cache` plugin cache) + Go adapter via `-github-action` argv. `packages/github-action/src/{download,plugin-cache}.ts`
- `actioncatalog`: `popular_actions.json`, 222 slugs, from `raw.githubusercontent.com/{slug}/{tag}{path}/action.{ext}`, pins via `git ls-remote`. `-d` detects new releases. `actioncatalog/generate/main.go:61`, `pins.go:26` → v2: this is what types `steps.<id>.outputs` for remote actions offline — **keep**, vendored by `xtask`
- `webhook`: generated offline from pinned languageservices schema + github/docs tables + octokit OpenAPI payloads. Manifest pins SHA-256s. `webhook/generate/README.md:1-60` → ADR-0014 §4 replaces the docs scrape. **keep** the octokit OpenAPI payload *types* — `webhooks.json` carries names only
- `rulecatalog`: one `[]rule.Descriptor` → `docs/rules.md`. `check-docs` keeps `Output:` blocks and playground links in sync. `rulecatalog/catalog.go:10` → v2 `RuleMeta` + `xtask rules-doc`
- `conformance` (`//go:build conformance`): pinned zips of languageservices, runner, SchemaStore, yaml-test-suite. Scores. Assessment staleness. `conformance/sources.json` → ADR-0017 absorbs
- `doctor`: build info, cwd, config path/error, tool availability. `cli/doctor.go:30-108`
- Completions bash/zsh/fish/powershell, `auto` via `$SHELL` then `$PSModulePath`, accepts a full shell path. `cli/completion.go:42-120`
- Playground: `GOOS=js GOARCH=wasm` via Vite plugin. ShellCheck in a JS worker bridged to Go. `playground/vite.config.ts:22-105`

### Done well

- Problem matcher regexp interleaves optional ANSI escapes between every field so coloured output still matches. `tools/matcher/object.mjs:1-14`
- SARIF 2.1.0: `columnKind: unicodeCodePoints`, relative → `%SRCROOT%`, absolute → `file:` URI (UNC-safe), fixes grouped per artifact, related locations, `properties.diagnosticCode`, PascalCase rule names + helpUri. `render/sarif.go:117-240`
- GitHub annotations: separate escape sets for data vs properties. Multi-line endLine adjusted when end col == 1. `render/result.go:149-170`
- LSP generation counter: each edit cancels in-flight analysis, snapshots open docs, drops stale results. `didChange` rejects version ≤ current. Republishes old targets once to clear removed deps. Diagnostics admitted only if digest matches captured revision. `langserver/server.go:68-228` → exactly ADR-0003/0004/0013
- Code actions validated against open buffers before offering. Versioned workspace edits. `langserver/actions.go:14-40`
- Config `extends` merge: later base wins, leaf wins. Mappings merge, lists/scalars replace, null resets. Unknown keys rejected. `config/overlay.go:48-60`
- Schema JSON from Go doc comments + `jsonschema:` tags. ShellCheck directives via `$ref` to a versioned schema. `tools/config/README.md`
- Action binary pin: input → `version-file` (`.tool-versions`) → bundled. Checksums file then asset, SHA-256, `wx` create, 120 s timeout, proxy agent, `ACTIONLINT_ACTION_BINARY` override. `packages/github-action/src/{version,tools,download}.ts`
- `--output-file` written to temp then atomic publish: `renameat2 RENAME_NOREPLACE` / `renamex_np RENAME_EXCL` / `MoveFileEx`. Refuses when output is also an input. `cli/report_destination.go:14-98`
- Pretty renderer: runewidth wrap, tab → 4 cells, control chars → U+FFFD, long lines windowed keeping caret at ~⅓. `render/pretty.go`
- Colour: `GITHUB_ACTIONS=true` forces colour for text formats when stdout is not a regular file. Windows VT enable. `cli/color.go:42-66`
- Help: flags grouped by annotation, OSC-8 hyperlinks when supported, `--json` help emits a machine-readable command description. `cli/help.go:143-328`
- `bump-version`: declared targets with expected occurrence counts, CHANGELOG validation, Nix build check, all-or-nothing. `tools/release/bump-version/README.md`
- Action PR review: diff-hunk parsing, comments only on changed lines, whole-fix `suggestion` blocks, dedup, verifies source == PR head. `packages/github-action/src/review.ts:45-410`
- Go action persists a versioned result JSON even on failure. `githubaction/action.go:50-55`
- Plugin project: declarations (config) vs pins (lockfile) separate. Read-only checks never activate undeclared lock entries. `localapp/plugin_project.go:17-184`

### Surprising

- Bare `actionlint` prints help to **stdout** — already ADR-0011. `cli/command.go:30-35`
- "Pretty output" is two things: `--output-format=pretty`, **and** JSON pretty-printing by shelling out to `jq --color-output` on a TTY (2 s timeout). `cli/json_terminal.go:33-62` → v2: no
- A file named `check`/`config`/`rules`/… in cwd wins over the subcommand. `--command NAME` forces. `cli/root_args.go:84-99` → v2: no
- `pre-commit` whitelists unknown flags because `git rebase --root` passes `--root` to hooks. `cli/root.go:92-99`
- `--pyflakes` remaps to `ruff --select F`. `cli/env.go:100-107`
- `fix` without `--apply` dumps the JSON plan. `cli/check.go:138-158`
- `--runner-snapshot FILE`: 8 MiB-capped JSON evidence with `freshness fresh|stale|missing` for self-hosted label resolution. `cli/runners.go:33-70` → v2 `[runner]` config section
- `config init` downloads and verifies default plugin artifacts before writing config. `localapp/config_migration.go:338` → v2: no
- Summary says "workflows" even when counting actions. `cli/summary.go:14-18`
- SARIF `tool.driver.name` is `"GitHub Actions lint"`. `render/sarif.go:235`
- LSP README admits completion uses a separate parser without per-job types. Hover/definition/symbols/semantic tokens unimplemented. `$/cancelRequest` is a no-op. Only `completions[0]` used. `langserver/README.md:17-45`
- Action Go adapter has its own Go-template renderers. Github template hard-codes `::error` for every severity. `githubaction/render.go:12-27`
- Action passes inputs **positionally** as argv. `githubaction/main.go:25`
- Popular-actions generator hand-patches two actions' metadata. `actioncatalog/generate/main.go:228-245`
- Webhook manifest keeps schema-only activities `image_version:{created,deleted,ready}` and **`pull_request:stacked`** with `production_verified: false`. `webhook/generate/sources/manifest.json:14-40` — this is where `github.event.pull_request.stack` in `autofix.yml:131` comes from: schema-declared, unverified
- Playground wasm `catalogueDigest` injected at build to reject catalogue/wasm mismatch. `playground/main.go:16-17`
- No `.pre-commit-hooks.yaml` in tree despite the subcommand.

## 4. Plugins, tool integration, registry, test infrastructure

`internal/plugins` 4.0k source / **10.6k generated** (wacogo bindings) / 5.6k test. `internal/tools` 6.2k / 7.7k. `pluginregistry` 1.6k / 1.6k. `analyzerhost` 0.8k / 1.8k. Public `plugin` SDK 78 lines. Rust `actionlint-extension-api` 522. testdata: projects 228, err 209, examples 105, ok 95, action_metadata 13, config 9, reusable_workflow_metadata 9, format 6, plugins 6, bench 5, conformance 2, realworld 2.

WIT package `kjanat:actionlint@0.2.0` interfaces: `invocation-context`, `analyzer`, `host`, `prepared-scripts`, `runner-provider`, `script-engine`, `source-annotations`, `source-files`, `template-effects`, `tool-integration`. Worlds: `plugin`, `annotated-plugin`, `managed-plugin`, `runner-plugin`, `script-engine-plugin`, `template-script-engine-plugin`. Guest fixtures: 14 plugins (Rust, C/wasi-sdk `no-sudo`, TinyGo `insecure-download`), 4 contract-shape, 8 component WAT, 13 core WAT.

### Preserve (as requirements for ADR-0009, not as the old contract)

- Guest declares `requires: list<fact>` (`source|scripts|mappings|invocations|execution|dependencies`), host supplies only those. `wit/analyzer.wit:213,301` → ADR-0009 does this through the world's import list
- `analyze` returns partial diagnostics **plus** an error — both legal. `analyzer.wit:320` → keep
- `script.spans`: per-text-line `{line,start,end,document,document-end}` with the exact lookup algorithm in the doc comment, incl. escape width and zero-width spans. `analyzer.wit:129` → this is the `scripts` provider's source map (architecture.md "Aliases, flow collections, folded scalars")
- `diagnostic.fixes[].revisions: list<{path,digest}>` — fixes are already revision-pinned at the WIT boundary. `analyzer.wit:260,275` → ADR-0007 identical
- `mapping.entries[].state: unknown|known|invalid`, `evaluated` = came from an object expression, "must not be interpreted again". `analyzer.wit:194` → the `${{ insert }}` / expression-valued-mapping model in ADR-0017
- `invocation-context`: `summary{entry, budget{max-depth,max-calls,max-scripts}, calls, scripts, stop, entry-interruption, complete}`, paged `calls(offset, limit 1..256)`. `call.outcome = expanded|opaque|unavailable|unresolved|dynamic|cycle|depth-limited|interrupted`. `condition = enabled|skipped|runtime-dependent`. Occurrence ids 64 lowercase hex. `analyzer.wit:5,62,106` → ADR-0006 `Occurrence` + the `Unresolved(reason)` node set — **adopt the outcome enum verbatim**
- `prepared-scripts.prepare(occurrence, dialect)` returns `text, sanitized, dialect, shell, startup, lines[script(u32)|comment|startup], excluded`. Shell-preparation profile 0.2.0 is normative: runtime shell beats declared. `# shellcheck shell=` overrides. Explicit bash → `set -e -o pipefail`. `${{ }}` → `_` same width. Default exclusions SC1091, SC2194, SC2050, SC2153, SC2154, SC2157, SC2043. Semantic change requires a new profile version. `prepared-scripts.wit:14,33`, `schemas/shell-preparation-0.2.0.md` → v2: the *preparation* is a core provider (facts about scripts), the ShellCheck-specific exclusions move to the extension. Versioned-profile idea **keep**
- `source-annotations.comments(reference)` with `placement: leading|trailing`, scalar-content comments excluded. `source-annotations.wit:24` → a `comments` provider
- `source-files.roots() -> list<{host, guest}>` with longest-component-prefix translation both ways. `source-files.wit:10` → keep for preopens
- `tool-integration.negotiate(config, options) -> {options, resources: list<workspace|directory>}`, error ⇒ native fallback. `tool-integration.wit:5,29` → extension manifest config schema (ADR-0010)
- `template-effects`: `effect = none|line-length|complete-references|opaque-name|opaque-parentheses|string-contents|docstring-summary|exported-name|percent-conversion`. Hosts must never infer effect from rule code/message. `template-effects.wit:39,70` → the Ruff masking contract. Becomes extension-side, but the *concept* (a diagnostic says what template-dependent effect it relies on) is worth a generic field on `Diagnostic`
- `runner-provider.resolve(request) -> resolution = resolved(candidates)|no-match(rejections)|unknown|not-applicable`. `candidate{name, os, architecture, default-shell, case-sensitivity, sh-is-dash, ephemeral}`. No-match needs fresh + complete evidence. `runner.wit:35-64` → `runner_facts` provider shape. `[runner]` config
- Limits: component 10 s, 1024 pages (64 MiB), lift 8 MiB / 262144 values. Core 10 s, 4096 pages, 1 min compile, pool of 4. Stdout/stderr 1 MiB per call, 64 KiB per line. Script-engine 8 MiB in/out, 10000 diagnostics. ≤4 script-engine modules per session. `component/options.go:12`, `core/engine.go:33`, `component/output.go:30`, `component/script_engine.go:23`, `analyzerhost/script_engines.go:12` → ADR-0009 limits section, same numbers as defaults
- Aggregate memory budget counts linear memories **and** reserved table maxima (32 B/entry) across nested core instances. Tables without finite max rejected. `component/memory.go:14`, `wasmbudget/tables.go:10` → `StoreLimitsBuilder` can't express this. Keep as a load-time check on the component type
- Imports allowed: own interfaces + `wasi:cli/ clocks/ io/ random/ filesystem/`. Wrong own-interface version → "this host provides X". `component/validation.go:13,123`
- Plugin name may not collide with a built-in rule nor contain `,`, CR/LF, `--`. `validation.go:15` → v2 extension namespace rule (ADR-0015)
- Descriptor option schemas must be self-bundled (no remote `$ref`). `analyzerhost/options.go:12`
- Artifact sources: path, `file://`, `http(s)://…#sha256=<64hex>`, `sha256://` (cache-only). Unpinned remote execution refused. 30 s, 128 MiB, SHA-256 over distributed bytes, cache re-hashed. `actionlint.plugin` custom section ≤1 MiB sniffed without compiling. `artifact/resolve.go:23-252`, `inspect.go:16` → keep all of it
- Registry admission: HTTPS dir URL, ≤5 redirects inside catalogue, JSON ≤4 MiB. Version record: tag == version, OIDC issuer + repo + owner ids match, `ref == refs/tags/<v>`, commit match, artifact sha == submission sha. Pin URL exactly a GitHub release asset or GitLab generic package. `contract = <interface>#sha256=<wit-digest>`. Status `active|deprecated|withdrawn|revoked`. Lock file ≤4 MiB, symlink ancestors refused, `Sync` offline, immutable version replacement refused, `latest` never downgrades. `pluginregistry/{client,model,status,lock}.go` → ADR-0009 "distribution/registry out of scope" — this is the spec when it comes in scope
- Tool integration: `tools.Script` = prepared snippet + source map "not a workflow AST visitor". Backend protocol transport-independent (process/core-wasm/component) routed by advertised `script_shells`/`script_inputs`. `tools/tools.go:32-83`, `backend/backend.go:23` → the extension boundary
- Source map: `sourcemap.Scalar{Value, Spans[{Start,End,Position,Width,LineBreak}]}` — folded whitespace has **no** span, escapes have nonzero `Width`. EOF end snaps to end of last mapped physical line. `yaml/sourcemap/scalar.go:14`, `workflow/ast.go:84` → architecture.md source map, same shape
- Fixture format: `testdata/{err,examples}/<name>.yaml` + `.out`, one line per diagnostic sorted by `findingmodel.Compare`, `/…/` = regex else exact, count must match. `projects/<name>/` + `.out`. `ok/` = zero diagnostics. `command/compatibility.json` CLI goldens. `format/` SARIF/JSON/JSONL/MD goldens. `integration/workflow/fixtures_test.go:171-321` → v2 `fixtures/rules/<area>/<name>/` keeps `.expected` as this line format for diffability, plus JSON for occurrence ids
- Fuzz: `FuzzParse`, `FuzzCheck`, `FuzzExprParse`, `FuzzGlobGitRef`, `FuzzGlobFilePath`. `fuzz/check_test.go:19` → `cargo fuzz` targets with the same names

### Done well

- **Contract digest**: `sha256(LF-normalised analyzer.wit)` embedded by the Go generator and recomputed by the Rust `build.rs`. Registry pin `contract` = `<interface>#sha256=<digest>` — compatibility is a hash, not a version. `tools/plugin-contract/main.go:87`, `build.rs:8`, `pluginregistry/model.go:247` → ADR-0009 uses WIT package semver. **add** the digest as a secondary check on load so a silently edited WIT is caught
- **Structural shape check**: loader rebuilds the expected component type and runs `CheckInstantiation`, catching wrong record/enum/signature even when the export name matches. `component/contract.go:12`
- **Per-call instance isolation**: fresh host/WASI/stdin/import instances per invocation. Teardown closes dependents before providers and survives panics. `component/call.go:48`, `call_teardown.go:13` → ADR-0009 one `Store` per call
- Memory-budget panic path recovered in `invoke` because wazero has no error return. `component/memory.go:61` → wasmtime `ResourceLimiter` returns `Result`, no panic needed
- Core-engine pool discards an instance after trap/limit. Test proves the next call gets a fresh one. `core/engine_test.go:331`
- **Host-owned preparation** exposed to guests so plugins never re-implement sanitising. `shellcheck/prepared_plugin.go:12`
- `${{ }}` → underscores, not spaces — spaces broke `if ${{…}}; then`. `analysis_shellcheck_run.go:80`
- Generated-line map with 0 = synthesised. Shebang kept on line 1. EOF+1 handled. `shellcheck_script.go:44,100`
- Fix safety: only literal (`|`) blocks get text edits. One unchanged line per replacement. Generated/expression/indent-changing → fix dropped. `shellcheck_fixes.go:25` → ADR-0007 `Applicability`: these are the `Safe` criteria for script fixes
- **Ruff template masks**: substitutions replaced by independent opaque identifiers preserving Unicode columns. Findings filtered by `(effect, exact mask range)` not by message. Scripts where masking can't keep Python syntax are skipped. Code→effect table pinned to a reviewed ruff version. `ruff/checker.go:362-378`, `template_effects.go:28`
- **Conformance ground truth**: `testdata/conformance/scalar-source/reference.json` = 18 recorded request/responses from the real runner's YAML/template parser at `67f01c27…`. Live mode re-runs against a Docker image. `scalar-source/README.md:3,17` → ADR-0017 fixtures
- Realworld corpus: 1.4 MB zip of 2021 GitHub workflows. `testdata/realworld/dataset.zip` → keep as a smoke corpus (nothing consumes it today)
- **CI change-planner**: `CI_FILTERS` JSON of `shared` + per-job globs. `CI: full` / `CI-Skip:` trailers. Any parse failure ⇒ run everything. Job table in step summary. `.github/ci/plan.mjs:6-113`
- Makefile: `--no-builtin-rules` (GNU make's `%.out: %` once deleted fixtures). Race-detector probe for broken ARM64 kernels. Auto-disable race under `CLAUDECODE=1`. `Makefile:1,45,52`
- Fixtures built from Rust (wasm32-wasip2), C (wasi-sdk reactor), TinyGo (compose WASI imports), WAT via `wasm-tools parse`. `Makefile:191`, `examples/plugins/*/build.sh` → ADR-0009 C guest uses the same toolchain
- forbidigo forces all FS reads through `fsread.Reader`. `.golangci.toml:46` → v2 `workspace` `os` feature is the equivalent boundary
- Nix check validates installed man/completions/schema `$ref` closure and runs an offline integration lint. `nix/check.nix:30`
- Installer verifies GitHub build-provenance attestation when `gh` is available. `distribution/install/download-actionlint.bash:130`
- Docker test asserts plugins work as non-root **offline**. `distribution/docker/test-docker-plugins.bash:2`

### Surprising

- Generated code dwarfs hand-written 10.6k vs 4.0k in `internal/plugins`. → ADR-0012 bans this
- `source-files.roots()` exists but preopens come from a context key, not the WIT call. `component/files.go:13`
- Public `plugin` package is 78 lines of type aliases. Its doc still says engines follow "the actionlint-plugin-ruff JSON ABI". `plugin/plugin.go:2`
- Legacy compat kept: unversioned core metadata accepted. Shell names lower-cased and `.exe` stripped. "legacy engines without routes inherit native selectors". `core/engine.go:305,323`, `backend/input_routing.go:190`
- Candidate (`file:`) registries bypass OIDC identity checks entirely. `pluginregistry/client.go:229`
- Lock file sits under `.github/` but the code refuses to rediscover it there because a checked-in `.github` symlink could redirect writes. `lock.go:112,191`
- `RUFF_OUTPUT_FILE` must be unset or ruff silently bypasses the stdout protocol. `ruff/checker.go:79`
- Ruff adapter hard-fails on any version other than 0.17.0 for template effects. `ruff/template_effects.go:20`
- `testdata/realworld/dataset.zip` committed, no consumer references it.
- Fuzz corpus dir does not exist. No crashers ever committed.
- `misbehave` fixture doubles as the oversized-descriptor fixtures via cargo features. `testdata/plugins/misbehave/Cargo.toml:13`
- `prepared-scripts.wit` tells guests they "must not … infer its results from the host version, package name, or diagnostic messages" — anti-fingerprinting written into the contract. `prepared-scripts.wit:5` → keep that sentence in ADR-0009
- `nix/integration.yml` is the smoke workflow used by both nix check and docker tests. `nix/integration.yml:7`
- Docker action keeps a 9-positional-arg protocol where empty values are errors. `distribution/docker/docker-action.sh:2`
