// Package provider describes tool providers as data. A manifest names the rule
// a tool implements, its transport, how to invoke it and how to decode its
// output. The host registers tools from manifests and never names a tool.
//
// This package is data only: it imports no configuration loader, no process
// runner and no WebAssembly runtime. Configuration reaches a provider as values
// passed in by the composition layer.
package provider

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"
	"actionlint.kjanat.dev/v2/rule"
)

// SchemaVersion is the only manifest schema version this host reads.
const SchemaVersion = 1

// Role selects which analysis contract a provider implements.
type Role string

const (
	// RoleScript checks extracted script snippets.
	RoleScript Role = "script"
	// RoleDocument analyzes whole workflow documents.
	RoleDocument Role = "document"
	// RoleRunner replaces a runner-side check.
	RoleRunner Role = "runner"
)

// Transport selects how the host reaches the provider's engine.
type Transport string

const (
	// TransportProcess runs a native executable once per check.
	TransportProcess Transport = "process"
	// TransportWasmCore loads a core WebAssembly module speaking the JSON ABI.
	TransportWasmCore Transport = "wasm-core"
	// TransportWasmComponent loads a WebAssembly component.
	TransportWasmComponent Transport = "wasm-component"
	// TransportEmbedded calls a Go function supplied by the embedding program.
	TransportEmbedded Transport = "embedded"
)

// Scope states where a provider's findings may be suppressed.
type Scope string

const (
	// ScopeSource findings belong to the script source itself.
	ScopeSource Scope = "source"
	// ScopeComposite findings may also belong to composite action inputs.
	ScopeComposite Scope = "composite"
)

// Manifest is one tool provider. Rule.Name is the tool id used by
// configuration (tools.<name>), rule policy and the command line.
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Version       string          `json:"version"`
	Role          Role            `json:"role"`
	Rule          rule.Descriptor `json:"rule"`
	Scope         Scope           `json:"scope"`
	Transport     Transport       `json:"transport"`
	// Artifacts lists per-platform executables or modules; empty means the
	// process entry is discovered on PATH.
	Artifacts []Artifact `json:"artifacts,omitempty"`
	// Preparation names the host preparation profile that masks templates and
	// supplies argument variables.
	Preparation  string       `json:"preparation"`
	Selectors    Selectors    `json:"selectors"`
	Capabilities Capabilities `json:"capabilities"`
	// ConfigSchema validates tools.<name>.config for every transport.
	ConfigSchema json.RawMessage `json:"config_schema,omitempty"`
	// ConfigShorthands maps keys users may also write directly under
	// tools.<name> to the ConfigSchema property they set. The host folds them
	// into tools.<name>.config while decoding.
	ConfigShorthands map[string]ConfigShorthand `json:"config_shorthands,omitempty"`
	// Process is present exactly when Transport is TransportProcess.
	Process *ProcessSpec `json:"process,omitempty"`
	// Integration names the managed document-analyzer contract a component
	// provides on the deprecated managed route. On a process provider it
	// names the contract of a component that may replace it, falling back
	// to the process for configurations the component declines.
	Integration *Integration `json:"integration,omitempty"`
	// DeprecatedFlags are retired command-line flags that still select this
	// provider. The command line accepts each one, hidden, for one release.
	DeprecatedFlags []DeprecatedFlag `json:"deprecated_flags,omitempty"`
}

// DeprecatedFlag is a retired command-line flag that selects a provider.
// --NAME= and --NAME=false disable the provider. Any other value runs the
// provider's default command with Arguments added. Either way, --tool,
// --no-tool and the provider's environment variables take precedence.
type DeprecatedFlag struct {
	// Name is the flag without leading dashes.
	Name string `json:"name"`
	// Arguments are added to the default command line.
	Arguments []string `json:"arguments,omitempty"`
}

// ConfigShorthand is a key written directly under tools.<name> that sets one
// property of the provider's configuration. Its type lives in the leaf
// configschema package, which configuration decoding shares.
type ConfigShorthand = configschema.Shorthand

// ReadsConfigFiles reports whether the provider reads native configuration
// files, so tools.<name>.config may name a path instead of inline settings.
func (m Manifest) ReadsConfigFiles() bool {
	return m.Process != nil && m.Process.ConfigFiles != nil && len(m.Process.ConfigFiles.Names) > 0
}

// Artifact is one content-addressed file for one platform.
type Artifact struct {
	// Platform is "<os>/<arch>", "wasm32/wasip2" or "wasm32/core".
	Platform string `json:"platform"`
	// Digest is "sha256:<64 lowercase hex>".
	Digest string `json:"digest"`
	// Filename is the materialized name, keeping ".exe" on Windows.
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type,omitempty"`
}

// Selectors choose the scripts a provider receives.
type Selectors struct {
	// Shells pre-filters run scripts by canonical shell basename. Empty leaves
	// selection to the preparation profile.
	Shells []string `json:"shells,omitempty"`
	// Inputs routes action inputs that carry source code.
	Inputs []analyzer.ScriptInput `json:"inputs,omitempty"`
	// Filename is the virtual snippet name the tool expects, such as
	// "actionlint.py".
	Filename string `json:"filename,omitempty"`
}

// Severity is a host severity a tool severity maps to.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Resource is a host resource a provider may request while checking.
type Resource string

const (
	ResourceWorkspace Resource = "workspace"
	ResourceDirectory Resource = "directory"
)

// Capabilities declare what a provider's findings may contain.
type Capabilities struct {
	// TemplateContract is the template-effects contract the provider honours.
	TemplateContract string `json:"template_contract,omitempty"`
	Fixes            bool   `json:"fixes"`
	// ReportedSources permits findings in files the tool read itself.
	ReportedSources bool       `json:"reported_sources"`
	Resources       []Resource `json:"resources,omitempty"`
	// MessageTemplate formats finding messages from {code}, {severity}, {row},
	// {column} and {message}; row and column are script coordinates.
	MessageTemplate string `json:"message_template,omitempty"`
	// Severities maps tool severities to host severities. Empty passes tool
	// severities through.
	Severities map[string]Severity `json:"severities,omitempty"`
}

// HostSeverity maps a tool severity through Severities. Without a table
// the severity passes through; a severity the table does not list maps to
// SeverityWarning and reports false.
func (c Capabilities) HostSeverity(severity string) (Severity, bool) {
	if len(c.Severities) == 0 {
		return Severity(severity), true
	}
	if mapped, ok := c.Severities[severity]; ok {
		return mapped, true
	}
	return SeverityWarning, false
}

// Stdin selects what a process reads on standard input.
type Stdin string

const (
	// StdinSource sends the prepared script source.
	StdinSource Stdin = "source"
	// StdinRequest sends the JSON analyzer.ScriptRequest.
	StdinRequest Stdin = "request"
)

// Concurrency selects how many checks of one process provider run at once.
type Concurrency string

const (
	// ConcurrencyPool shares the host process pool.
	ConcurrencyPool Concurrency = "pool"
	// ConcurrencySerial runs one check at a time.
	ConcurrencySerial Concurrency = "serial"
)

// ProcessSpec describes a native executable transport.
type ProcessSpec struct {
	// Entry is an artifact filename or a bare command discovered on PATH.
	Entry string `json:"entry"`
	// Args are literal tokens and closed-grammar placeholders: {var:NAME},
	// {flag:NAME:--opt} (two arguments), {option:NAME:--opt} (one argument,
	// --opt=VALUE), {switch:NAME:--on:--off}, {list:NAME:SEP},
	// {config:toml-pairs:--opt}, {config:toml-file}, {config:json-file} and
	// {user-args}. Placeholders are substituted, never evaluated.
	Args  []string          `json:"args"`
	Stdin Stdin             `json:"stdin"`
	Env   map[string]string `json:"env,omitempty"`
	// EnvPassthrough names ambient variables copied into the scrubbed child
	// environment.
	EnvPassthrough []string `json:"env_passthrough,omitempty"`
	// EnvUnset names variables removed from the child environment whatever
	// sets them: Env, EnvPassthrough or the command's configured environment.
	// It keeps settings that redirect or suppress the tool's output away from
	// the process.
	EnvUnset []string `json:"env_unset,omitempty"`
	// ExitCodes are accepted when stdout is non-empty; empty means [0].
	ExitCodes   []int           `json:"exit_codes,omitempty"`
	Config      ConfigRendering `json:"config"`
	ConfigFiles *ConfigFiles    `json:"config_files,omitempty"`
	// ConfigSchema further restricts tools.<name>.config on this transport.
	ConfigSchema json.RawMessage `json:"config_schema,omitempty"`
	Version      *VersionProbe   `json:"version,omitempty"`
	Output       Output          `json:"output"`
	// TemplateEffects maps diagnostic codes to template effects. It is honoured
	// only when the probed version equals Version.TemplateEffects.
	TemplateEffects map[string]analyzer.ScriptTemplateEffect `json:"template_effects,omitempty"`
	UserArgs        *UserArgs                                `json:"user_args,omitempty"`
	// Concurrency defaults to ConcurrencyPool.
	Concurrency Concurrency `json:"concurrency,omitempty"`
}

// Render selects how engine configuration reaches the process.
type Render string

const (
	RenderNone      Render = "none"
	RenderJSONFile  Render = "json-file"
	RenderTOMLFile  Render = "toml-file"
	RenderTOMLPairs Render = "toml-pairs"
	RenderJSONStdin Render = "json-stdin"
)

// ConfigRendering turns host configuration into engine configuration.
type ConfigRendering struct {
	// Transform maps host config JSON to {"config": ..., "vars": {...}}; vars
	// override preparation variables and a null var drops it.
	Transform *Transform `json:"transform,omitempty"`
	Render    Render     `json:"render"`
	// Forbidden lists dotted key paths rejected in the rendered configuration.
	Forbidden []string `json:"forbidden,omitempty"`
}

// ConfigSearch is one place native configuration files are looked up.
type ConfigSearch string

const (
	// ConfigSearchUpward walks from the working directory to the project root.
	ConfigSearchUpward ConfigSearch = "upward"
	// ConfigSearchXDG looks in the XDG configuration directory.
	ConfigSearchXDG ConfigSearch = "xdg"
	// ConfigSearchHome looks in the user's home directory. Windows has no
	// home-directory dotfile convention, so it is skipped there; per-user
	// files live in the directory ConfigSearchXDG covers.
	ConfigSearchHome ConfigSearch = "home"
)

// ConfigFiles names native configuration files and where policy may look.
type ConfigFiles struct {
	Names  []string       `json:"names"`
	Search []ConfigSearch `json:"search,omitempty"`
}

// OutputFormat selects how stdout is decoded.
type OutputFormat string

const (
	// FormatEnvelope is the {ok, diagnostics, errors} check result envelope.
	FormatEnvelope OutputFormat = "script-diagnostics@1"
	// FormatSARIF is a SARIF 2.1.0 log.
	FormatSARIF OutputFormat = "sarif@2.1.0"
	// FormatJSON is tool-specific JSON reshaped by a transform.
	FormatJSON OutputFormat = "json"
	// FormatText is tool-specific text reshaped by a transform.
	FormatText OutputFormat = "text"
)

// Output describes a process's stdout.
type Output struct {
	Format OutputFormat `json:"format"`
	// Transform is required for FormatJSON and FormatText and forbidden
	// otherwise.
	Transform   *Transform   `json:"transform,omitempty"`
	Coordinates *Coordinates `json:"coordinates,omitempty"`
}

// RowConvention states which line breaks a tool counts.
type RowConvention string

const (
	// RowsLF counts LF-delimited rows.
	RowsLF RowConvention = "lf"
	// RowsSource counts LF, CRLF and lone CR as line breaks.
	RowsSource RowConvention = "source"
)

// ColumnUnit states what a tool's columns count.
type ColumnUnit string

const (
	ColumnsChars ColumnUnit = "chars"
	ColumnsBytes ColumnUnit = "bytes"
	ColumnsUTF16 ColumnUnit = "utf16"
)

// EndConvention states whether end positions include their last character.
type EndConvention string

const (
	EndExclusive EndConvention = "exclusive"
	EndInclusive EndConvention = "inclusive"
)

// Coordinates describe a tool's position convention. Omitted means
// lf/chars/exclusive, the analyzer.ScriptPosition contract.
type Coordinates struct {
	Rows    RowConvention `json:"rows"`
	Columns ColumnUnit    `json:"columns"`
	End     EndConvention `json:"end"`
}

// TransformLanguage names a transform program language.
type TransformLanguage string

// LanguageJQ is the jq language as implemented by gojq.
const LanguageJQ TransformLanguage = "jq"

// Transform is a program reshaping JSON. Exactly one of Digest and Inline is
// set; a digest names a content-addressed program.
type Transform struct {
	Language TransformLanguage `json:"language"`
	Digest   string            `json:"digest,omitempty"`
	Inline   string            `json:"inline,omitempty"`
}

// VersionProbe identifies the installed engine version.
type VersionProbe struct {
	Args []string `json:"args"`
	// Pattern is an RE2 expression with exactly one capture group.
	Pattern string `json:"pattern"`
	Minimum string `json:"minimum,omitempty"`
	// TemplateEffects is the exact version the template-effect table was
	// reviewed against.
	TemplateEffects string `json:"template_effects,omitempty"`
}

// UserArgs constrains arguments users add to a process command line.
type UserArgs struct {
	// Operands permits arguments that are not options; "-" and "--" are
	// operands.
	Operands bool `json:"operands"`
	// ValueOptions are long options whose value may be the next argument.
	// A value option without a value is rejected.
	ValueOptions []string `json:"value_options,omitempty"`
	// ValueShorts are short option letters consuming the rest of their
	// cluster or the next argument.
	ValueShorts string `json:"value_shorts,omitempty"`
	// AttachedShorts are short option letters whose optional value must be
	// attached; each ends its cluster.
	AttachedShorts string `json:"attached_shorts,omitempty"`
	// InlineOptions are value options whose value must be an inline
	// KEY = VALUE setting. A value naming an existing file is rejected.
	InlineOptions []string  `json:"inline_options,omitempty"`
	Forbidden     []ArgRule `json:"forbidden,omitempty"`
	// Overrides are user options that replace a preparation variable.
	Overrides []ArgOverride `json:"overrides,omitempty"`
	// EnvOptions name environment variables the tool reads as extra options;
	// they are scanned like user arguments.
	EnvOptions []string `json:"env_options,omitempty"`
	// Launchers are commands that start the tool, such as an interpreter
	// running it as a module. When the configured executable is a launcher,
	// its arguments up to the tool are kept ahead of the manifest arguments
	// and only the rest are user arguments.
	Launchers []Launcher `json:"launchers,omitempty"`
}

// Launcher is a command that runs the tool. Its options end where an operand
// names the tool, or where its module option names the tool's module.
type Launcher struct {
	// Command matches the launcher's lowercase base name without ".exe"; it is
	// an RE2 expression anchored at both ends.
	Command string `json:"command"`
	// Flags are options taking no value.
	Flags []string `json:"flags,omitempty"`
	// ValueOptions are options whose value is attached with "=" or is the
	// next argument.
	ValueOptions []string `json:"value_options,omitempty"`
	// Choices restrict the values of value options.
	Choices map[string][]string `json:"choices,omitempty"`
	// ShortFlags are clustered short option letters taking no value.
	ShortFlags string `json:"short_flags,omitempty"`
	// ShortValues are short option letters consuming the rest of their
	// cluster or the next, nonempty argument.
	ShortValues string `json:"short_values,omitempty"`
	// Assignments permits NAME=VALUE arguments setting the tool's
	// environment, except the variables Protected names, compared without
	// case.
	Assignments bool     `json:"assignments,omitempty"`
	Protected   []string `json:"protected,omitempty"`
	// Selector is an RE2 expression, anchored at both ends, for at most one
	// argument selecting the launcher's runtime version.
	Selector string `json:"selector,omitempty"`
	// Separator lets "--" end the options; the next argument must name the
	// tool.
	Separator bool `json:"separator,omitempty"`
	// Tool is an RE2 expression, anchored at both ends, an operand naming the
	// tool must match.
	Tool string `json:"tool,omitempty"`
	// Module is a short option letter whose value names the tool's module, as
	// Python's -m does.
	Module *LauncherModule `json:"module,omitempty"`
	// Hint completes the error for arguments the launcher does not accept.
	Hint string `json:"hint"`
}

// LauncherModule is a short option naming the module that runs the tool.
type LauncherModule struct {
	Short string `json:"short"`
	Name  string `json:"name"`
}

// ArgRule rejects user arguments. Exactly one of Flag, Prefix and ShortLetter
// is set.
type ArgRule struct {
	// Flag matches a long option before any "=".
	Flag string `json:"flag,omitempty"`
	// Prefix matches any argument starting with it.
	Prefix string `json:"prefix,omitempty"`
	// ShortLetter matches the letter anywhere in a short-option cluster.
	ShortLetter string `json:"short_letter,omitempty"`
	Reason      string `json:"reason"`
}

// ArgOverride states that a user option replaces a preparation variable.
type ArgOverride struct {
	Flag  string `json:"flag"`
	Short string `json:"short,omitempty"`
	Drops string `json:"drops"`
}

// Integration names the managed document-analyzer contract of a component.
type Integration struct {
	Tool     string `json:"tool"`
	Contract string `json:"contract"`
}
