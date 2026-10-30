# Oracle verdict: tool providers (revised) + acceptance vs diagnostics

Read this session: `docs/design/tool-providers.md`, `critique.md`, ADR-0017 (`rust-adr`), `parse.go` (helpers 95-160, 166-262, 286-400, 835-900, 1075-1135, 1256-1266, 1406-1416, 1700-1716, 1770-1790, 1900-1912, 1985-2005, 2046-2141), `parse_finding.go`, `parse_yaml_compatibility.go`, `codec/bridge.go:110-130`, `codec/graph.go:40-70`, `rulecatalog/{catalog,registry,reference}.go`, `config/{catalog.go:1-80,lint.go:20-110,config_default.yaml,catalog_test.go,lint_contract_test.go:25-45}`, `rule/rule.go`, `diagnostic/diagnostic.go:20-23`, `expressions/{matrix.go:55-135,fields.go}`, `matrix/resolution.go:100-120,189-278`, `diagnosticpolicy/collect.go:20-80`, `conformance/{conformance_test.go:1-120,conformance_adapters_test.go:1-60,sources.json,testdata/*}`, `backend/backend.go:98-135,155-225`, `ruff/{backend_process.go:85-170,adapter.go:58-150,registry.go:28-50,command.go:190-260}`, `shellcheck/{analysis_shellcheck_run.go:118-145,250-300,registry.go:28-45,shellcheck_fixes.go:25-40}`, `selection/requirements.go:14-80`, `adapters/registry.go:14-40`, `localapp/{plugins.go:36-52,plugin_selection.go:295-310}`, `cli/plugin.go:66-96`, `analyzerhost/process.go:30-48`, `analysis/analysis.go:30-70`, `analyzer/backend.go:100-149`, `tools/config/main.go:20-30,210-240`, `ci.yml:170-180,355-360,430-434,465-470`, `go-yaml/v4 yaml.go:596,613` (signatures only). Everything else is **inferred** and marked.

---

## Part A — Revised tool-provider design

### A.1 Critic items: decision and concrete change

Verified true unless noted. Format: **item → decision → change (stage)**.

**(a) Leaks**

| # | Item | Decision | Change |
|---|---|---|---|
| a1 | `inputFilename` table `backend.go:214-225`, literal `"actionlint-script"` `:162`, Ruff rejects any name but `actionlint.py` (`backend_process.go:92`), ShellCheck uses `actionlint.sh` (`:281`) | **Accept** | `Manifest.Selectors.Filename string` (virtual snippet name the tool expects, e.g. `actionlint.py`). Extension for action inputs is language data, so `Profile.Extension() string` replaces the switch; runner builds `actionlint-input/<lang>/<action>/<input>.<ext>` from the profile. Stage 2 |
| a2 | `plugin/plugin.go:2-3` package doc | Accept | Stage 5 docs list |
| a3 | `prepared-scripts.wit:26` `excluded: list<u32>` in SC numbering | Accept | Stage 7 owns it: `script-engine@0.3.0` carries `excluded: list<string>`; the deprecated managed world keeps `u32` until retired (K4) |
| a4 | `testutil/plugin_registry.go:145`, `fixtures.go:64,70,80` | Accept | Stage 4 sweep; helpers take an id parameter |
| a5 | `config.go:61` ignore-regex example | **Reject** | It is a user-facing example of matching message text produced by data; not a code leak. Keep |
| a6 | `"style"` severity cases in three renderers | **Accept** | Severity is an open string on the wire. Manifest `Capabilities.Severities map[string]string` maps tool severities to host `error|warning|info` (bundled ShellCheck: `{"error":"error","warning":"warning","info":"info","style":"info"}`, bundled Ruff passthrough). Unmapped value → `warning` + debug log. Renderers lose their `"style"` case in stage 4 (same stage the mapping lands) |
| a7 | `docs/design/{tool-configuration-ownership,package-layout}.md` | Accept | Stage 5 docs list |

**(b) Generic parts not wired**

| # | Item | Decision | Change |
|---|---|---|---|
| b1 | `backend.Configure`/`WithScriptInputs`/`WithConfiguredEngine`/`configuredRouting` (`adapters/registry.go:31-33`) | Accept | `provider.Register` returns `backend.Configure(reg)`. `input_routing.go` stays in `internal/tools/backend`, unchanged. Stage 4 |
| b2 | No coordinate convention; `scriptcoords` only inside Ruff (`backend_process.go:156-169`) | Accept | `Output.Coordinates{Rows: "lf"\|"source" , Columns: "chars"\|"bytes"\|"utf16", End: "exclusive"\|"inclusive"}`; default `lf/chars/exclusive` (= `analyzer/backend.go:129-131` contract). `process.Backend` normalizes via `scriptcoords` *before* returning, so the Decoder stays pure. Stage 2 |
| b3 | `RequireProcessOptIn` (`adapter.go:63`), `ProjectMayNeedProcess` (`requirements.go:18`) | Accept | Both move to `provider/selection.go` with `ConfigVariants`; `configEnabled` (`requirements.go:69-72`) reads `cfg.Tools[name].Enabled`. Stage 1 (temporary map read) → Stage 4 (move) |
| b4 | `Register` must derive `NeedsProcess`, `RequiresExecution`, `ConfiguredBackend` | Accept | `NeedsProcess` = `Transport == process && sel.Explicit.Backend == nil`. `RequiresExecution` = `len(Capabilities.Resources) > 0` (ShellCheck lists `workspace,directory`; Ruff none — matches `shellcheck/registry.go:33` vs Ruff; the field's exact semantics **inferred**, not read). `ConfiguredBackend` = unchanged `configuredRouting` reading `tools.<id>.backend`. Stage 4 |

**(c) Behaviour that would be deleted — every one receives a destination**

| # | Behaviour | Destination (all data or generic runner; stage 2 unless noted) |
|---|---|---|
| c1 | Ruff forbidden flags (`command.go:194-259`) | `ProcessSpec.UserArgs{Forbidden []ArgRule}`; closed rule kinds: `{flag}`, `{prefix:"@"}`, `{short_letter:"o"}`, `{option_without_value}`, `{operands:false}`, `{reason}`. Bundled Ruff manifest encodes every branch of `validateCommand` 1:1; port `command_test.go` as a golden over the data |
| c2 | Ruff write/IO config-key denylist (`native_options.go`, not read) | `ConfigRendering.Forbidden []string` (key paths), enforced by the host on the *rendered* config after the jq transform, never by jq |
| c3 | Ruff output identity (`backend_process.go:155-163`, `SameFile`) | Host-side: a decoded diagnostic's `filename` must equal `$vars.filename` or be listed in `sources` (when `reported_sources`). Host adds `$vars.filename_abs` (= `Dir/filename`) so the jq program can compare exact strings; `SameFile` symlink equivalence is **deliberately dropped** (Ruff echoes the path it was given) |
| c4 | Ruff `select: []` → `ALL/ALL` (`checker.go:344-348`), `--target-version` suppressed when native config sets it (`:350`) | Config transform output becomes `{config: ..., vars: {...}}`; `vars` overrides profile vars (`target_version: null` drops the `{flag:...}`). Both tricks are ~4 lines of jq in the bundled Ruff `config.jq` |
| c5 | Ruff reversed-range fallback (`adapter.go:143-145`) vs runner error (`backend.go:197-200`) | Generic runner: unmapped start → key position; reversed or unmapped end → drop end. No error. Debug log |
| c6 | Ruff optional version gate (`adapter.go:74-84`) vs runner join (`backend.go:156-161`) | `process.Backend.ValidateTemplates` returns `(false, nil)` + `Warn` when `!sel.Required`; runner already `continue`s on `!available` |
| c7 | ShellCheck line-0 semantics (`analysis_shellcheck_run.go:253-259`) vs runner (`backend.go:190-193`) | `Prepared.LineMap` returns `{Line int, Origin: script\|directive\|startup}`. Runner: `directive` → error `tools.<id>.config: <code>: <msg>`; `startup` → dropped unless mapped severity `error`; only `script` lines produce findings |
| c8 | Fix description = untrimmed message (`shellcheck_fixes.go:30`) | Bundled jq: `description: .message` (untrimmed) |
| c9 | `--shell` precedence (`:141`), `SHELLCHECK_OPTS` (`shellcheck_options.go:57-60`) | `UserArgs.Overrides: [{flag:"--shell", drops:"dialect"}]`; host scans user args *and* `UserArgs.EnvOptions: ["SHELLCHECK_OPTS"]`. Bundled manifest `env_passthrough: ["SHELLCHECK_OPTS"]`; the Backend logs inherited values as today (`:209-211`) |
| c10 | rc discovery XDG/home (`shellcheck_paths.go`) | `ConfigFiles{Names []string, Search: ["upward","xdg","home"]}` — host feature keyed by data |
| c11 | Backend-path rejection of rc/external-sources (`analysis_shellcheck.go:111-124`) | Per-transport schema: `Manifest.ConfigSchema` (all transports) + `ProcessSpec.ConfigSchema` (process extras). Rejection = JSON-schema validation of `tools.<id>.config` against the selected transport's schema |
| c12 | Script selection by dialect or `# shellcheck shell=` (`:122-127`) vs basename pre-filter (`backend.go:110-116`) | `Prepared.Skip string` — profile decides. `selectors.shells` is a pre-filter only when non-empty; bundled ShellCheck leaves it empty and the shell profile selects. Stage 3 |
| c13 | No user-args slot | `{user-args}` placeholder; Ruff places it after config, ShellCheck first — as today |
| c14 | No working directory on `Check` (`analyzer/backend.go:117-121`) | `ScriptRequest.Directory string` (optional; wire in 0.3.0, stage 7). `process.Backend` sets `Execution.Dir` per request. Sources are relative to it |
| c15 | ShellCheck concurrency (pool today, serial in runner `backend.go:163`) | Runner fans `Check` calls through `errgroup` bounded by `ProcessPool` for process transports; `ProcessSpec.Concurrency: "pool"\|"serial"`, default `pool`. Wasm backends serial (engine thread-safety **inferred**) |

**(d) Claims**

| Item | Decision |
|---|---|
| K8 `go:embed` false (`config/generate.go:46`, `actioncatalog/embedded.go:7` embed in-package) | **Accept.** Bundled data lives in `internal/tools/provider/bundled/{embed.go,shellcheck/,ruff/}`. No public `providers` package |
| K9 partly false (Ruff `Required` true when options nil, `ruff/registry.go:44-47`; ShellCheck false, `shellcheck/registry.go:38-41`) | **Accept.** Corrected statement: today the two tools disagree; the CLI default (`env.go:98-99`) is what makes Ruff optional. New rule is uniform: explicit selection → required; bundled default → optional. No manifest field |
| 11770d95 incomplete (pins resolved at `plugin_selection.go:301` before `loadPlugins`; raw `--plugin` has no id; `Pin.Name/Alias` ≠ tool id) | **Accept.** Disabling by id is evaluated at three points: (1) bundled manifests — by id, before anything; (2) locked pins — `Pin.Name/Alias` match at `plugin_selection.go:301` *before* `Resolve`; after stage 6 also by manifest digest; (3) raw paths/URLs and v1-lock pins with no matching alias — after metadata load, before instantiation (`plugins.go:46`), matching `rule.name`. Honest claim: "never downloaded" holds for bundled + alias-matched pins; others are resolved but not loaded. `DisabledLanguages`/`declaresLanguage` replaced in stage 5, **not reverted now** (CI stays green; the fix it carries is real) |
| `ScriptReplacement` JSON tags | **Bug, accept.** `Row int \`json:"row"\``, `EndRow \`json:"end_row"\``, `Column \`json:"column"\``, `EndColumn \`json:"end_column"\``, `Text \`json:"text"\`` |
| `plugin_test.go:143` passes `--no-shellcheck` | Accept; stage 5 |
| K4 gap: default `plugins: [shellcheck, ruff]` (`config_default.yaml:6`) makes the managed component the main path | **Accept.** `provider.FromComponentMetadata(meta) Manifest` derives `Integration{Tool: <engine/rule name>, Contract: <tool-integration version>}` from the component's static metadata at load; the managed route is first-class this release, deprecated in docs only. Retirement unchanged (K4) |
| Unauthorised decisions | Decided below (A.2) |

**(e) Missing to ship**

| Item | Decision |
|---|---|
| Binary+jq usable only at stage 6 (`Sniff` rejects non-wasm) | **Accept; re-ordered.** Stage 2 adds a *local manifest source*: `--tool ID=path/to/manifest.json`, `tools.<id>.manifest: <path>`, PATH-discovered binary, no artifacts, no store. Usable end of stage 2 |
| K2 for repo-pinned binaries on untrusted PRs | Process transports selected by repository config or lockfile need `--allow-config-processes`; the Action never sets it; Wasm needs nothing. Documented in `plugins.md` |
| Public API | Stage 4: `analysis.Options.Providers []analysis.Provider{Manifest plugin.Manifest, Backend analyzer.ScriptBackend, Process *analysis.Process}`; `plugin.ParseManifest([]byte) (Manifest, error)` (re-export of `provider.Manifest`). `analysis.Options.{ShellCheck,Ruff}` become deprecated aliases (A.2). Examples updated |
| CI/Docker/Action/packaging users (`ci.yml:175,358,381-383,432,468,481,588`, `docker-nightly.yml:88-90`, `distribution/docker/*`, `packages/github-action/src/runtime.ts:55-59`, `tools/release/check-packaged-cli.bash:20,28`, `action.yml:53-62`) | Added to stage 5 as an explicit table; migrated to `--tool/--no-tool` in stage 5b. `Dockerfile:23-46` **keeps** bundling shellcheck/ruff — they are the bundled providers' PATH defaults |
| ~140 test matches | Aliases make stage 5a zero-churn; stage 5b is the sweep |
| Config schema (`actionlint.schema.json:8842,9293`, `tools/config/main.go:24,213-235`) | Stage 1: the generator stitches each bundled manifest's `config_schema` under `tools.<id>.config` (Ruff's = today's reflected text moved into `bundled/ruff/config.schema.json`; ShellCheck's `$ref`s `schemas/shellcheck/0.11.0.schema.json`). Installed providers: `actionlint config schema` (stage 6) emits the stitched schema |
| Docs | `docs/rules.md` (generated), `api.md`, `lsp.md`, `config-migration.md`, `design/{tool-configuration-ownership,package-layout,analysis-contracts}.md`, README, changelog → stage 5 |
| Tests | Add: arch guard `internal/tools/provider` ⊄ wazero/wacogo (`contracts_test.go:140-151` pattern); playground `GOOS=js` build with gojq (pure Go, **inferred**); K7 fixture covers fix eligibility (`shellcheck_fixes.go:34`); parity snapshots include c5, c6, c7, c4 cases |
| LSP (`cli/lsp.go`, `localapp/editor_session.go`) | Stage 5 |

### A.2 Surface compatibility — decided

**Deprecated aliases for exactly this release, then removed in the next (Rust host).** Not hard removal, not keep.

- Flags `--ruff`, `--shellcheck`, `--no-ruff`, `--no-shellcheck`, `--pyflakes`: hidden, each prints one `deprecated: use --tool/--no-tool` warning, mapped 1:1 (`--ruff=CMD` → `--tool ruff=CMD`, `--ruff=` → `--no-tool ruff`, `--pyflakes` → `--tool ruff` + warning).
- Env `ACTIONLINT_RUFF_*`/`ACTIONLINT_SHELLCHECK_*`: read as aliases of `ACTIONLINT_TOOL_RUFF_*` etc. (`externalToolEnvironment` is already prefix-parameterised `env.go:201-231`), new names win on conflict.
- Action inputs `shellcheck`, `ruff`, `pyflakes`: kept and mapped to `disable-tools`/`tools`; `action.yml` marks them `deprecationMessage`.
- `analysis.Options.{ShellCheck,Ruff}`: kept, deprecated, folded into `Providers`.
- Ruff launcher grammar: the *grammar* (executable + user flags, shell-word split) is kept as `{user-args}` + `UserArgs.Forbidden` data; the Go in `ruff/command.go` is deleted. Users lose nothing.
- Reason: 140 test sites + 8 external consumers make a hard cut unreviewable in one PR; a one-release alias costs ~60 lines and lets stage 5b be a mechanical sweep.
- **11770d95: not reverted now; replaced in stage 5** (above).

### A.3 Knots re-cut

- **K1** unchanged (gojq) + config transform outputs `{config, vars}` (c4) + `Output.Coordinates` (b2).
- **K2** + `env_passthrough` is the only route for ambient env (`SHELLCHECK_OPTS`); untrusted-PR policy above.
- **K3** unchanged.
- **K4** + `FromComponentMetadata` derivation; managed route is primary this release.
- **K5 changed**: deprecated aliases (A.2). `docs/migration-v2.md` table lists old → new.
- **K6** scope unchanged (stages 0-6 + host side of 7), but binary+jq usable at end of stage 2.
- **K7** unchanged + fix-eligibility fixture.
- **K8 changed**: `internal/tools/provider/bundled`.
- **K9 corrected** wording; decision unchanged.

### A.4 Stage order (changes only)

0 → 1 → **2 (+ local manifest source, + `ScriptRequest.Directory`, + concurrency, + c1-c11,c13 data fields)** → 3 (+ `Prepared.Skip`, `LineMap` origins) → 4 (+ public API, severity map, renderer cleanup, parity gate incl. c4-c7) → **5a aliases + CI/Docker/Action/packaging/LSP + 11770d95 replacement** → **5b test/docs sweep** → 6 (+ `config schema`) → 7 (+ `excluded: list<string>`, `directory`).

---

## Part B — Acceptance vs diagnostics

### B.1 Model

Two concepts, two mechanisms, one catalog vocabulary:

1. **Acceptance** = "GitHub's service parser/converter rejects this document." One rule, `syntax-check` (stays `correctness/syntax`, stays configurable at `lint.rules.correctness.syntax-check`, `config/catalog.go:64`). Every acceptance error gains a stable **code**: `parser.SyntaxError.Code` (new field, `parse_finding.go:14-19`) projected to `diagnostic.Diagnostic.Code` (`diagnostic.go:21-23`, already defined as "a diagnostic kind within Rule"). Codes are a closed Go const set in `internal/workflow/parser/codes.go`: `yaml`, `bom`, `multiple-documents`, `unknown-tag`, `tag-value`, `unknown-key`, `missing-key`, `type-mismatch`, `empty-value`, `flow-null`, `alias`, `expression-form`. This is exactly how `ruff`/`shellcheck` already name their kinds (`Code: d.Code`, `adapter.go:146`) — no catalog explosion, and `ignores:`/inline suppression keep working by rule. Truth = the service verdict (ADR-0017 consequence 1): an acceptance error with no measured rejection is a bug by definition; unmeasured sites keep their error *and* get a code so they can be flipped one fixture at a time.
2. **Named lint** = accepted by the service, but pointless/suspicious. Ordinary `rule.Descriptor` in `rulecatalog/catalog.go`, flat kebab name like `job-needs`, `if-cond` (no `job/empty-steps` slashes — Go names are flat; ADR names map 1:1 in the Rust host). New category **`style`** with group `lint.rules.style` (`LintRulesConfig`, `lint.go:27-35`): `Recommended: true`, severity `warning`. Why not `suspicious`: that group is opt-in by contract (`docs/config.md:206-209`, `lint_contract_test.go:35`), and users who relied on the old errors must still be told by default. Why not `correctness`: nothing is wrong, it is pointless.

Config-side changes for the model: `lint.go` + `Style Group`, `Groups()`, `catalog_test.go:15` passes automatically, `docs/config.md:148-240`, JSON schema regen, `docs/rules.md` regen (`rulecatalog/generate`).

### B.2 The ten cases

| Case | New behaviour | Rule / code / category / default | Change lands | Test |
|---|---|---|---|---|
| `run: ! y` (service rejects, go-v2 silent) | Acceptance error "tag of a YAML scalar must be one of … but got \"!\"" | `syntax-check` / `unknown-tag` / correctness / error | `RawYAMLTagError` `parse.go:1096-1119`. **Must verify first** how `go-yaml/v4` represents the non-specific `!` tag on a plain scalar (**inferred**: it resolves to `!!str` and the check at `:1096` swallows it). If so, `codec/bridge.go:33` must carry the raw tag spelling (`syntax.Node.RawTag`) and `RawYAMLTagError` rejects `RawTag == "!"` | parser unit + service fixture `reject` |
| second `---` document | Acceptance error at the second document's mark: "GitHub Actions reads one YAML document; remove the document starting here" | `syntax-check` / `multiple-documents` / correctness / error | `codec/bridge.go:118` gains `NextDocument(data) (Mark, bool)` using `upstream.NewDecoder` (`yaml.go:596,613`) decoding twice; `parser.Parse` `parse.go:2115-2122` calls it. Workflows only (action loaders unmeasured) | codec unit + service fixture `reject` |
| leading BOM | Acceptance error at 1:1: "a byte order mark makes GitHub read the first key as \"\uFEFFon\"; remove it" | `syntax-check` / `bom` / correctness / error | `parser.Parse` `parse.go:2115`: `bytes.HasPrefix(b, "\xEF\xBB\xBF")`. Workflow-only; `internal/action` untouched (ADR: manifest loaders strip BOM) | parser unit + service fixture `reject` |
| empty `on:` | No acceptance error. Named lint "workflow declares no triggers and never runs" | **`empty-triggers`** / style/triggers / warning, recommended | `parseEvents` `parse.go:856-893`: null scalar → `[]Event{}` without `parseString(n,false)` (`:836` currently errors "string should not be empty"); drop `checkNotEmpty` at `:888`; check whether `parseSectionMapping("on", n, false, true)` `:865` rejects an empty mapping (**not read**). Lint emitted by new `internal/workflow/shape` checker (see B.4) | service fixture `accept` + expected `empty-triggers` |
| empty `run:` | No acceptance error. Lint "step runs an empty script" | **`empty-run`** / style/steps / warning, recommended | Delete `parse.go:1779-1782`; `shape` checker emits on `ExecRun.Run.Value == ""` with source scalar | service fixture `accept` + `empty-run`; update `parse_test.go:68` |
| job with `runs-on`, no `steps` | No acceptance error. Lint "job has no steps and does nothing" | **`empty-job`** / style/jobs / warning, recommended; also covers `steps: []` (`checkSequence("steps", n, false)` `:1770` → `allowEmpty=true`) | Delete `parse.go:1995-1997`; keep `"runs-on" section is missing` (`:1998-2000`, unmeasured → acceptance, code `missing-key`) | service fixture `accept` + `empty-job` |
| `max-parallel: .5` | Accepted; integer parse truncates toward zero (runner converter: `1.5 → 1`, ADR). Lint "value .5 is truncated to 0" | **`numeric-values`** / correctness/values / warning, recommended; codes `truncated`, `non-positive` | `parseInt` `parse.go:309-311` accepts `YamlTagFloat`, truncates; the `> 0` checks at `:1260-1263` and `:1410-1413` move from acceptance into `numeric-values` (`non-positive`) because they are unmeasured and `.5 → 0` would otherwise re-reject the fixture | service fixture `accept` + `numeric-values`; parser unit for `1.5 → 1`, `0x10 → 16` |
| `timeout-minutes: ${{ false }}` | No parse-time change: `parseFloat` already returns an expression (`:378-386`); the error comes from `checkNumberTy` `fields.go:67-75` under the expression checker's rule (which descriptor `Checker.Errorf` uses — **not read**, find with `rg` in `expressions/checker.go`). That is already a named finding, not acceptance | existing expression rule, unchanged | Nothing | service fixture `accept` + allowed finding of that rule. Fixture format must distinguish acceptance from named findings (B.5) |
| bare `if: True` | Accepted; condition is `true` | none | `parseCondition` `parse.go:229-238`: `if n.Tag == YamlTagBool { s.Value = strings.ToLower(n.Value) }` (`FALSE`, `True`, `Null` all normalise; `!!null` already handled `:231`) | service fixture `accept`, no findings; quoted `"True"` stays a reject fixture whose go-v2 verdict is an `expression-reference` finding (gap entry stays) |
| matrix from `include: ${{ fromJSON(...) }}` → "property X is not defined" | No finding for `matrix.*` | none (false positive removed) | `matrixTypeFromResolutionWithInferredRows` `expressions/matrix.go:83-104`: loosen when `!resolution.IncludesComplete()`, i.e. `if resolution.Open() \|\| !resolution.IncludesComplete() { object.Loose() }` at `:101-103` (today only `Open()` looses; an include expression sets `includeKnown=false` at `resolution.go:268,278` but `ReachableNames()` is then empty → strict empty object) | `matrix_inference_test.go` case + service fixture `accept` |

### B.3 Other parser errors to move under the model

Codes first (mechanical, no behaviour change), flips only with fixtures:

| Site | Today | Code / disposition |
|---|---|---|
| `HandleYAMLUnmarshalError` `parse.go:2101-2112` | "could not parse as YAML" | `yaml` |
| `inspectYAMLAliases`/`admitYAMLAliasUses` `:2126-2132` | alias errors | `alias` |
| `checkFlowMappingNulls` `parse_yaml_compatibility.go:14-27` | flow-null error | `flow-null` (measured: runner absorbs the colon — keep) |
| `unexpectedKey` `:148-160` | unknown/expected key | `unknown-key` |
| `"on"`/`"jobs" section is missing` `:2097-2102`, `"runs-on" section is missing` `:1998`, `uses`-only key errors `:1985-2005` | missing/forbidden keys | `missing-key` / `unknown-key`; unmeasured → stay acceptance |
| `typeErrorf` sites (`CheckString :189`, `parseBool :288`, `parseInt :311`, `parseFloat :370`, matrix row `:1147`, `checkSequence :175`) | "expected … node with … tag" | `type-mismatch`; `parseInt` float case flips to `numeric-values` (B.2) |
| `RawYAMLTagError` `:1096-1119` | tag errors | `unknown-tag` / `tag-value` (measured by ADR: `!!str` ok, `!!bool yes` fails — keep) |
| `missingExpression` `:199-201` | "expecting a single ${{...}} expression" | `expression-form` |
| `CheckString` empty `:192-195` (every `parseString(n,false)` caller: event names `:836`, `uses`, `runs-on`, etc.) | "string should not be empty" | `empty-value`; each caller flips to a style lint only with a fixture |
| `checkNotEmpty` `:166-171` other callers | "section should not be empty" | `empty-value`; same rule |
| `schedule event must be configured with mapping` `:840` | | `type-mismatch`; stays |

### B.4 Where the named lints live

New package `internal/workflow/shape` (`check.go`): one `Checker` emitting `empty-triggers`, `empty-job`, `empty-run`, `numeric-values` from the parsed tree, registered where `job-needs` is registered (registration site **not read**; find with `rg '"job-needs"' --type go -g '!*_test.go'`). Catalog entries in `rulecatalog/catalog.go` + `config/catalog.go` `Rules()` with `Category: "style"` (numeric-values: `correctness`). `docs/rules.md` regenerated.

### B.5 Tests

- **Service fixtures (truth = service verdict)**: `testdata/conformance/service/{cases.json, *.yml}` with rows `{id, runny_row, runner_sha: "1ba885c2", input, verdict: accept|reject, findings: [rule ids allowed]}` — hand-vendored for these ten now, `internal/conformance/fetch` vendors `runny/corpus/regressions.jsonl` once runny is public. Harness `internal/application/service_conformance_test.go` (no build tag, runs in ordinary CI): `reject` ⇒ at least one `syntax-check` finding; `accept` ⇒ zero `syntax-check` findings and every other finding's rule ∈ `findings`. Differs from `internal/conformance` (`//go:build conformance`, fetched cache, `differences.json` baseline): that suite stays as is; entries it holds for these cases (e.g. `basic.json/empty_expression/0`, `differences.json:4`) are removed as each flips.
- Parser units for each B.2 row; `parse_test.go:68` updated; integration snapshots that assert the old messages (`internal/integration/cli/check_test.go`, `internal/application/stdin_syntax_test.go`) updated to the new rule ids.
- Staging for green CI: (B0) `Code` field + codes at helper level, no message change; `style` group + catalog + docs regen, no emitters. (B1) `shape` checker + parser loosening in one commit with its test updates. (B2) BOM, multi-doc, `!`. (B3) `parseCondition`, matrix loosen, `numeric-values`. (B4) service fixture harness, then flip remaining `empty-value` sites one fixture at a time.

---

## Part C — Ordered work list (this Go release)

First three are small enough to start now.

1. **`ScriptReplacement` JSON tags + `SyntaxError.Code` → `Diagnostic.Code` + `parser/codes.go`** (B0a). Pure additive; no message changes.
2. **`parseCondition` YAML-bool normalisation** (`parse.go:229`) + parser unit + first service fixture file with that single `accept` row. Establishes the harness.
3. **Matrix include-expression loosen** (`matrix.go:101-103`) + `matrix_inference_test.go` case + fixture.
4. `style` group (`lint.go`), catalog entries for `empty-triggers`, `empty-job`, `empty-run`, `numeric-values` (no emitters), `docs/rules.md`/schema regen (B0b).
5. `internal/workflow/shape` checker + parser loosening (`:836,:888,:1779-1782,:1995-1997,:1770 allowEmpty`) + `parseInt` float truncation + range checks → `numeric-values` + test updates + fixtures (B1, B3).
6. BOM, multiple documents, `!` tag acceptance errors (B2) — after reading go-yaml's handling of `!`.
7. Assign codes to every remaining acceptance site (B.3) and remove flipped `differences.json` entries (B4).
8. Provider stage 0: manifest types incl. A.1 fields (`Selectors.Filename`, `Capabilities.Severities`, `Output.Coordinates`, `UserArgs`, `ConfigFiles`, `Concurrency`), schema, gojq/envelope/SARIF decoders, bundled data under `internal/tools/provider/bundled`, golden decode tests.
9. Stage 1: `ToolsConfig` map, schema stitching, selection reads `cfg.Tools[name]`.
10. Stage 2: generic process transport + runner absorption (c1-c11, c13-c15) + `ScriptRequest.Directory` + **local manifest source** (binary+jq usable here).
11. Stage 3: profiles (`Prepared.Skip`, `LineMap` origins, K7 masker + fix-eligibility fixture).
12. Stage 4: register from manifests, public API `Options.Providers`/`plugin.ParseManifest`, severity map + renderer cleanup, `FromComponentMetadata`, parity gate (snapshots include c4-c7), delete bespoke adapters, testutil literals.
13. Stage 5a: `--tool/--no-tool`, env prefix, doctor, Action inputs, deprecated aliases with warnings, 11770d95 replacement (three-point id disabling), LSP wiring.
14. Stage 5b: sweep of ~140 test sites, `ci.yml`/Docker/packaging/`runtime.ts`/`action.yml`, docs incl. `migration-v2.md`, `plugin/plugin.go` doc, design docs.
15. Stage 6: OCI pull-by-digest, `Materialize`, lock v2, `actionlint config schema`.
16. Stage 7 (host side): `script-engine@0.3.0` with `fixes`, `source`, `directory`, `excluded: list<string>`; 0.2.0 still accepted.

Not read, flagged for the implementer before touching: go-yaml's representation of a bare `!` tag; `parseSectionMapping` empty-mapping behaviour (`:865`); which rule descriptor `expressions.Checker.Errorf` reports under; the `job-needs` registration site; `tools.Registration.RequiresExecution` semantics; gojq compiler options and `GOOS=js` build.
