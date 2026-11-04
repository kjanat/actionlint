# Repository layout proposal

Actionlint reads inputs, understands them, runs checks, prepares results and delivers them. This proposal assigns each operation a place in the repository.

## What happens to an input

```text
Files, editor buffers, request and configuration
                         │
                         ▼
READ                     input/
                         Source text, source revisions, configuration
                         │
                         ▼
UNDERSTAND               languages/ + model/
                         Syntax, definitions, calls, types and known values
                         │
                         ▼
CHECK                    checking/
                         Findings and proposed edits
                         │
                         ▼
PREPARE RESULTS          results/
                         Validated findings, effective severity, summaries
                         │
                         ▼
DELIVER                  output/ or lsp/
                         Files, stdout, stderr or editor messages
```

These are responsibilities, not five mandatory whole-repository passes. A syntax check can run after parsing. A check involving a called action needs its definition and invocation context. Building that context can require another source read. A bad expression must not prevent unrelated jobs from being checked.

`checking/session.go` coordinates this work. It requests missing sources, builds the required models, runs enabled checks and assembles a result. CLI and LSP code submit requests to that same operation. They do not each build their own checking pipeline.

## Repository root

```text
actionlint/
├── cmd/
│   ├── actionlint/                   Native main function
│   └── actionlint-lsp-wasm/          Browser main function
├── internal/                        Private Go implementation, generators and tests
├── applications/
│   ├── playground/                  Browser UI and its tests
│   └── github-action/               TypeScript GitHub Action and its tests
├── libraries/
│   ├── browser-lsp/                 Worker transport and reusable browser client
│   └── github-changelog/            Independently published changelog reader
├── plugins/
│   ├── protocol/
│   │   ├── wit/                     Component interfaces
│   │   └── json/                    Metadata and process-message schemas
│   ├── sdk/
│   │   ├── go/
│   │   ├── rust/
│   │   └── typescript/
│   └── examples/                    Complete plugin examples, each with its own tests
├── distribution/
│   ├── npm/
│   ├── pypi/
│   ├── winget/
│   ├── docker/
│   ├── aur/
│   ├── install/                     User-facing installation scripts
│   └── nix/
├── tools/                           Maintenance scripts
├── docs/
├── schemas/                         Published result and tool schemas
├── .github/
├── action.yml                       GitHub-required action entry point
├── actionlint.schema.json           Generated configuration schema
├── go.mod
├── package.json                     One Bun workspace
├── Cargo.toml                       Rust workspace
├── .mise.toml                       Tool selection and installation
├── tasks.toml                       Build, check, generate and release commands
└── runner.toml                      Task-runner integration
```

Ordinary root documents, formatter configuration and lockfiles are omitted here. Required entry files such as `flake.nix` can refer to their implementation under distribution.

The root distinguishes products people run, libraries they import, plugin-author materials and installation formats. Host implementation is private. Public Go packages exist only to support documented plugin-author operations. Registry imports do not determine that public API.

## Private implementation

```text
internal/
├── cli/                             Interpret commands and return process status
├── lsp/                             Serve editor requests on native and browser hosts
├── input/                           Locate, read and identify the inputs
├── languages/                       Parse text and implement language behavior
├── model/                           Describe workflows, actions and their relationships
├── checking/                        Select and execute rules against those descriptions
├── results/                         Validate and process findings and proposed edits
├── output/                          Format results and write requested output
├── plugins/                         Install, load and invoke extensions
├── integrations/                    Access Git, GitHub, HTTP and external processes
└── development/                     Go generators and cross-subsystem Go tests
```

These directories have different outputs. `languages` produces syntax and language-level answers. `model` gives those answers workflow meaning. `checking` asks whether that meaning violates a rule. `results` processes what the rules report. No `documents/` directory hides workflows and actions, and script representations are not owned by the lint runner.

### Input: what was requested, and which bytes are being checked?

```text
input/
├── discover.go                      Directory traversal and file selection
├── read.go                          Disk, stdin and editor-buffer inputs
├── configuration/                   Load, merge and migrate configuration
│   └── schema/                      Construct the configuration schema
├── references/                      Resolve and obtain action/workflow dependencies
└── source/                          Captured text, identity and revision
    ├── position.go                  Byte offsets, lines and columns
    ├── aliases.go                   Accepted names for the same captured source
    └── mapping.go                   Correspondence between original and extracted text
```

Discovery applies inclusion/exclusion settings and repository boundaries. Reading captures bytes once for a particular revision. An editor buffer can replace disk contents for that request. Findings and edits refer to that captured revision, not an unrelated later file read.

Reference resolution returns a source or a classified outcome such as not found, access denied or unavailable. A caller must not treat all three as proof that an action does not exist. Git and API operations come from integrations. The dependency resolver's own request/response contract belongs with reference resolution; running it as a subprocess does not make it a lint-rule plugin.

Configuration loading does not import every rule implementation. Checking resolves names, levels and rule-specific options against loaded declarations once for each applicable source/invocation scope. The resulting rule policy controls both which checks run and how their findings are treated. Result processing receives that resolved policy rather than independently interpreting configuration again. Schema construction receives the same declarations used for option validation.

### Understand: syntax first, workflow meaning second

```text
languages/
├── yaml/                            YAML nodes, scalar decoding and source positions
├── json/                            Runner-compatible JSON reading and JSON value representation
└── expressions/
    ├── syntax/                      Tokens, parser and expression tree
    ├── types/                       Types, subtypes and type relationships
    ├── values/                      Runtime values and conversions
    ├── functions/                   Function contracts and their implementations
    ├── typecheck/                   Name/type checks without executing expressions
    └── evaluate/                    Evaluation order, short-circuiting and runtime outcomes

model/
├── workflows/                       Workflow fields, jobs, steps and matrices
├── actions/                         Action inputs, outputs and execution definitions
├── calls/                           Call targets, repeated invocations and input/output bindings
├── contexts/                        Available expression names and invocation-specific bindings
├── runners/                         Runner operating systems and capabilities
├── repositories/                    Repository facts used by checks
└── scripts/                         Extracted scripts, shell selection and source mappings
```

YAML decoding recognizes YAML structure and source locations. Workflow/action decoding assigns meaning to fields such as `jobs`, `runs`, `uses` and `with`. A YAML mapping is not itself a workflow job. Action kinds such as composite, JavaScript and Docker are represented within the action model; they are not competing definitions of what an action is.

The model records definitions separately from invocations. Two calls to the same action share its definition but can have different inputs, types, reachable steps and findings. Rules receive views of these relationships instead of reconstructing them from YAML nodes.

The model also distinguishes a known absence from something that could not be determined. Its construction reports unresolved references to the session, which requests more input when allowed. Model code does not initiate hidden network requests.

#### Expression boundaries

| Part             | Question it answers                                                                                                     | What it must not confuse with that answer                             |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `syntax`         | Can this text be parsed, and what operations does it express?                                                           | Whether a name exists or an operation can succeed.                    |
| `types`          | What values does a type describe, and how does it relate to another type?                                               | A particular runtime value or whether evaluating it succeeds.         |
| `values`         | What is this runtime value, and what does a specified conversion produce?                                               | The function's declared result type.                                  |
| `functions`      | Which arguments and conversions does this function accept, how are arguments demanded, and what result does it produce? | A policy warning that a legal conversion looks suspicious.            |
| `typecheck`      | Are names and argument types valid under those contracts? What result type follows?                                     | Running the expression to discover its value.                         |
| `evaluate`       | Which operations execute, in what order, and with what value or failure?                                                | Treating unknown values as false or unevaluated branches as executed. |
| `model/contexts` | Which names and values are available at this workflow location and invocation?                                          | Implementing a second expression evaluator.                           |

Function declarations are shared by type checking and evaluation. A function implementation is stored with its contract and uses an evaluation context supplied by the evaluator to request arguments. It does not import the concrete evaluator. The evaluator consumes function declarations, avoiding a dependency cycle. Syntax and type/value definitions do not depend on the rule runner.

A declared boolean return type and an evaluation failure can both be true facts about one call. A valid coercion must not become a type error merely because a separate rule considers it suspicious. A specific call can further refine a declared result type without replacing the function's contract.

`languages/json` is explicit because expression JSON behavior cannot be assumed to match Go's ordinary JSON decoder. GitHub's [FromJson implementation](https://raw.githubusercontent.com/actions/runner/main/src/Sdk/DTExpressions2/Expressions2/Sdk/Functions/FromJson.cs) uses Newtonsoft's reader with date parsing disabled and double parsing enabled, then converts the token to pipeline values. Parsing acceptance and value conversion need runner comparisons. Expression `fromJSON` uses that behavior; expression `toJSON` owns its own runner-compatible formatting in its function implementation. LSP, manifests and report serialization use their respective JSON contracts instead of inheriting expression-language quirks.

Scripts are extracted from workflow/action fields with their effective shell and a mapping back to the original text. That preparation belongs to the model. Executing ShellCheck or another script checker belongs to check execution and plugin hosting.

### Check: rules ask questions of the model

```text
checking/
├── session.go                       Coordinate source loading, model construction and checks
├── select.go                        Resolve enabled rules and validate their options
├── execute.go                       Supply declared inputs and invoke checks
├── registry.go                      Index declarations supplied by built-ins and plugins
└── rules/
    ├── declaration.go               Rule identity, category, defaults, options and input scope
    ├── correctness/                 Rule declarations and implementations
    ├── suspicious/                  Rule declarations and implementations
    └── security/                    Rule declarations and implementations
```

Rule directories follow the configuration categories. Rule maturity is a separate declaration field.

Each rule owns its declaration, implementation and tests together. The declaration supplies its current name, aliases and deprecation status, category, default severity, option schema and required input scope. A catalogue is an index of those declarations. There is no independently maintained `catalog/` copy of rule descriptions or defaults, and no separate `builtin/` wrapper around the same rules. Generated documentation and configuration schemas read that index.

`rules/declaration.go` defines the common declaration type, not every rule's metadata. Category packages use that type. The registry in the parent `checking` package imports the implementations and assembles their declarations; category packages do not import that parent. This dependency direction avoids a registry-to-rule-to-registry import cycle.

A rule declares whether it needs a syntax tree, a job graph, an action invocation or an extracted script. The executor supplies that input and records when it could not be constructed. A rule checking missing metadata inspects the resolution outcome directly.

Parsers and type checkers return structured issues alongside any usable output. Syntax and expression rules consume those outcomes, including failures with no usable tree. They translate language issues into rule findings using their own declarations. Deeper rules receive only the parts of the model their inputs require. Suppressing a parse-error finding does not turn a failed parse into a valid tree. Operational failures such as unreadable files use the failure path rather than being relabelled as syntax findings.

For example, the container-runner rule receives a job and its resolved runner capabilities:

```text
Model: job uses a container; runner operating system is Windows
Rule:  this runner does not support job containers
Finding:
    code              container-runner
    message           This job uses a container but selects a Windows runner.
    location          the container declaration
    related location  the runner selection
    proposed edits    none
```

The rule does not parse YAML, call the GitHub API or resolve runner labels. Those facts are shared model inputs. If runner support is unknown, the rule follows its declared uncertainty policy rather than turning unknown into unsupported. Tests cover that distinction.

A rule can produce a fix for one finding and no fix for another. Fix availability is a property of that finding's proposed edits. Rule documentation can explain examples and restrictions without declaring every finding automatically fixable.

### Prepare results: findings are data, not formatted text

```text
results/
├── finding.go                       Rule identity, message and related source locations
├── fix.go                           Proposed edits, applicability and expected source revisions
├── validate.go                      Check reported sources, ranges and edit bounds
├── severity.go                      Apply the rule policy resolved before execution
├── suppressions.go                  Match selectors and track whether they were used
├── fixes.go                         Build edit plans and reject conflicting edits
└── summary.go                       Sort/group findings and count them by file and rule
```

Every finding location, including related locations, identifies a captured source revision, either directly or through the result's source table. Validation checks those references and that edits address the expected revision. The source package owns coordinate arithmetic; results validation enforces the finding/edit contract. Invalid plugin output produces a plugin failure.

Resolved rule policy includes enabled state, validated options and severity overrides, including how overrides apply to provider-reported severities. Execution and result processing consume that same policy. Severity formatting does not decide whether a rule was enabled.

Suppression matching records which selectors matched findings. The executor then supplies these usage records to the unused-suppression rule.

Execution also records check completion for each relevant source and invocation. If a selected check could not run, absence of its findings does not prove that a matching suppression is unused. The unused-suppression rule consumes both usage and completion records and must distinguish proved-unused selectors from selectors whose usage could not be determined. Repeated invocations contribute to the same source-comment accounting. Disabled rules are an explicit selection outcome, not an execution failure.

Result processing produces findings, counts and check-completion information. The CLI maps that outcome to an exit code. The LSP maps it to protocol responses and diagnostics. I/O failures, plugin failures and cancellation are reported distinctly from ordinary lint findings.

Partial failures do not discard validated findings from completed checks. The result records both those findings and the incomplete work. Publication still respects request and document versions: an LSP response for a superseded buffer must not replace the current buffer's diagnostics.

### Deliver: format and destination are different choices

```text
output/
├── pretty.go                        Human-readable diagnostics with source excerpts
├── text.go                          Plain text diagnostics
├── oneline.go                       One line per finding
├── gcc.go                           GCC-style locations and messages
├── json.go                          Structured JSON report
├── jsonl.go                         One JSON record per line
├── sarif.go                         SARIF report
├── github.go                        Escaped GitHub workflow-command annotations
├── template.go                      User-supplied output templates
├── writers.go                       Dispatch selected renderers to supplied writers
└── apply.go                         Write approved edit plans after checking current revisions
```

A renderer receives prepared results and writes to a supplied writer. It does not choose stdout, open a destination file or rerun rules. The CLI selects and opens destinations, then supplies those writers to output dispatch. Repeated equivalent format requests are deduplicated before rendering. Operational messages use stderr; requested machine-readable output is not mixed with those messages. Explicitly selecting multiple formats for one stream produces those formats in the selected order, not one combined JSON document. LSP diagnostics bypass these report renderers: the LSP converts prepared results to protocol messages and sends them through its transport.

Applying source edits is a separate output operation from rendering a report. CLI fix handling calls it only when application was requested. The LSP returns workspace edits for the editor to apply.

## CLI and editor entry points

```text
cmd/actionlint/main.go               Start the native process
cmd/actionlint-lsp-wasm/main.go      Start the browser server

internal/cli/
├── root.go                          Top-level arguments, help and command dispatch
├── check.go                         Check command
├── fix.go                           Fix command
├── lsp.go                           Start the shared server over stdio
├── config/                          Configuration subcommands
├── plugin/                          Plugin subcommands
├── rules.go                         List and explain rules
├── doctor.go                        Diagnose setup and tool availability
├── completion.go                    Generate shell completion
└── version.go                       Version and build information

internal/lsp/
├── server.go                        Request dispatch and initialization
├── documents.go                     Open buffers, document versions and change notifications
├── diagnostics.go                   Convert results to LSP diagnostics
├── completion.go                    Completion requests
├── actions.go                       Quick fixes and suppression edits
├── cancellation.go                  Cancel superseded or cancelled requests
└── transport/                       Native stdio and browser message transport
```

The native and browser builds use the same LSP server and rule implementations. Their bootstraps supply transport and host capabilities: native filesystem/process access on the native host, and editor buffers, browser storage/network access and supported worker/plugin execution in the browser. Unavailable capabilities produce explicit incomplete-check or operation-failure outcomes. Tests exercise both sets of capabilities.

`libraries/browser-lsp` supplies the JavaScript worker/client connection and packages the compiled server. The playground consumes that library. A browser-capable VS Code extension can use the same server and transport contract.

`main.go` contains process startup, not every command. Commands translate requests and present results. Rule execution, reference resolution and plugin loading do not belong to command handlers.

## Plugins and external access

A plugin implements an actionlint extension contract. Its manifest declares its capabilities, its executable implements them, and downloadable files distribute it.

```text
internal/plugins/
├── manifests/                       Read and validate plugin declarations
├── artifacts/                       Inspect files, select platform variants and verify digests
├── installation/                    Registry lookup, downloads and cache population
├── protocol/                        Host-side messages and generated bindings
└── execution/
    ├── component/                   Invoke component-model Wasm plugins
    ├── module/                      Invoke core Wasm plugins
    └── process/                     Invoke process plugins through their protocol

internal/integrations/
├── github/                          GitHub REST/GraphQL requests and response interpretation
├── git/                             Repository and remote Git operations
├── http/                            Shared requests, authentication accounting and quota handling
└── process/                         Subprocess startup, streams, exit status and cancellation
```

OCI artifacts distribute plugin files. Installation retrieves and caches them, artifact inspection verifies their contents, and execution invokes the selected plugin contract.

Installation supplies retrieved bytes and expected digests to artifact inspection. Inspection verifies those digests, locates or extracts the manifest, and passes its bytes to manifest validation. Manifest validation checks declarations against the protocol schema. Loading checks the selected executable against its declared contract before using it for checks. Failures at these steps are installation or plugin failures, not workflow findings.

`integrations/github` calls GitHub's APIs. It supplies repository information to the input-loading operation. HTTP and Git access report their operations for network accounting; external resolvers must report the network operations they perform themselves.

Plugin checks pass through the same rule selection and result processing as built-in checks. Protocol conversion supplies their declared inputs and translates their responses.

Rule declarations select supported script languages and expose tool options. `model/scripts` supplies script text, effective shell and source mapping. Tool-specific invocation and output translation belong to the plugin implementation or manifest/transform.

Authoritative WIT and JSON definitions live under root `plugins/protocol`. Host bindings live internally. Plugin-author bindings live in the SDK. Both derive from those definitions. Platform-neutral wire values can be shared, but native host code must not import guest-only Wasm ABI bindings or initialization. Public SDK signatures do not expose private host types.

## Registry ownership

The registry is a separate repository. It owns publisher authentication, release records, catalogue publication and its private host-validation adapter. It does not determine actionlint's public Go surface.

For source-level validation, its unpublished adapter module can use a path beneath the actionlint module prefix, such as `actionlint.kjanat.dev/v2/registry/validation`. Ordinary registry packages can import that adapter because its own path has no `internal` segment. The adapter can import the host's internal packages because its import path is beneath their permitted parent.

Registry CI selects an exact actionlint revision. Its controlling build module requires both modules and uses build-local replacements for the adapter source and the pinned actionlint checkout. Replacements in dependency modules are ignored. A replacement locates source; it does not itself bypass an internal import restriction. Nested internal packages have their own narrower restriction. See [Go's internal-package rule](https://go.dev/doc/go1.4#internalpackages) and [module replacements](https://go.dev/ref/mod#go-mod-file-replace).

The registry updates and tests its adapter when private host code changes. First-party plugin host tests can use the same arrangement. Distributed plugins use supported plugin contracts. The private adapter build needs verification against the selected host revision.

## Tests, examples and development tools

Tests and fixtures live with the behavior they test. There is no root `testdata`, `fixtures` or shared fixture pool.

- Unit tests sit beside their implementation, with package-owned test data.
- SDK and example tests sit in their own packages and build using supported SDK APIs.
- Tests spanning several subsystems live under `internal/development/tests/integration`.
- Runner and Wasm reference comparisons live under `internal/development/tests/conformance`. Each comparison owns its input cases, reference execution and expected outcomes.
- A reused fixture has one owning suite; other suites consume it through an explicit test input.

`development/tests/conformance/wasm` compares plugin behavior between actionlint's host and Wasmtime. Runny comparisons belong under `development/tests/conformance/expressions` or the workflow suite whose behavior they test.

Plugin examples live under `plugins/examples/<example-name>` regardless of language. Each contains the source, manifest, build instructions and tests needed to run that example. There is no private second collection of Go examples. Deliberately malformed host-test programs belong to their test suites, not the example index.

```text
internal/development/
├── generators/
│   ├── config-schema/               Configuration types/declarations → actionlint.schema.json
│   └── rule-docs/                   Rule declarations → rule reference documentation
└── tests/
    ├── integration/                 Go tests spanning CLI, LSP and Action behavior
    └── conformance/                 Go comparisons with runner and Wasm references

tools/
├── release/
│   ├── release-candidate.mjs         Build and verify a proposed release
│   └── release-tag-recovery.mjs      Recover interrupted release-tag publication
└── toolchain/
    └── update-go-toolchain.mjs       Update the configured Go toolchain
```

Mise installs tools and runs build, generation and release commands. WIT bindings are generated by the existing binding generators invoked through mise tasks. Configuration-schema and rule-documentation generators use Go because they import Go types and rule declarations directly.

Release and maintenance scripts use JavaScript, TypeScript or shell according to the work they perform. Their tests sit beside them. JavaScript and TypeScript tooling uses the root Bun workspace. Go tests live beside the packages they exercise, or in the cross-subsystem suites above. Test suites own their generated fixtures.

Remove the internal architecture checker, its layer-policy generator, and their tasks, CI invocations and generated artifacts.

## What implementation must establish

1. Native CLI commands and both LSP hosts call the same checking operation.
2. Rules use shared syntax/model inputs. Declarations, option schemas, aliases and default levels have one source.
3. Parse errors, unknown values, runtime failures and policy warnings are distinct. Reference comparisons cover expression and JSON behavior.
4. Findings and fixes identify the captured source revision. Suppression accounting and edit-conflict checks work across built-in and plugin results.
5. Format selection is independent of destinations. CLI status, stderr failures and LSP diagnostics preserve their respective meanings.
6. Public SDK packages and examples build without private host imports. Registry validation builds against its selected host revision without expanding the public API.
7. Build tasks, workspace links, generated outputs and tests use the moved paths. No compatibility aliases preserve rejected development names.

## Review status

Terra and Sol independently reviewed the design and verified the corrected file at SHA-256 `0CD2F761D47A4D7C919C47A5C099150509318E84A57AB03A07D3ACE8E61CFF61`. Both reported their findings resolved and found no substantive contradictions. Only this review-status paragraph changed afterward. Review details are recorded in `.review-artifacts/repository-layout/input-to-output-review.md`.
