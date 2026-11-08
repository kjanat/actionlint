// Package tools defines script-tool registration independently of tool names and transports.
package tools

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflow "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// Context supplies invocation-owned resources to a registered adapter.
type Context struct {
	Config, ProjectConfig   *config.Config
	WorkingDir, ProjectRoot string
	Processes               *process.ProcessPool
	LoadEngine              func(context.Context, string) (checkinputs.ScriptBackend, error)
	Debug                   func(string, ...any)
	Warn                    func(error)
	// Skipped receives the rule name of an enabled check that could not run.
	// Providers may call it concurrently; disabling a check does not call it.
	Skipped func(string)
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
	Document             checkinputs.Input
}

// Registration owns a tool's selection and construction policy. The host only
// consumes capabilities; adding a provider does not change its control flow.
type Registration struct {
	InvocationDefinitions []commands.Definition
	// ToolIntegration identifies a negotiated document provider for this tool.
	ToolIntegration   *checkinputs.ToolIntegration
	Descriptor        rule.Descriptor
	Scope             rule.ConfigurationScope
	SupportsActions   bool
	ScriptInputs      []checkinputs.ScriptInput
	InputSelectors    func(Context) ([]checkinputs.ScriptInput, error)
	ConfiguredBackend func(Context) (checkinputs.ScriptBackend, error)
	RequiresExecution bool
	NeedsProcess      func(context.Context, *config.Config, *config.Config) (bool, error)
	Validate          func() error
	Enabled           func(Context) bool
	Required          func(Context) bool
	Build             func(Context) (Runner, error)
	WithBackend       func(checkinputs.ScriptBackend) (Registration, error)
	// PrepareInput shares adapter configuration with advertised action-input routes.
	PrepareInput func(context.Context, Script) (preparation.Prepared, error)
	// WithAnalyzer adapts a document analyzer to this tool's configuration and policy.
	WithAnalyzer func(checkinputs.Checker) (Registration, error)
}

type Runner interface {
	Analyze(context.Context, Batch) ([]Finding, error)
}

type Scope struct {
	Config                             *config.Config
	ConfigProvenance                   config.Provenance
	AnalysisDir, Workspace, ActionPath string
	Reader                             *files.Reader
	Composite                          bool
}

type Execution struct {
	Shell          string
	ImplicitShell  bool
	Directory      string
	DirectoryKnown bool
}

// Script is a prepared snippet and its source map, not a workflow AST visitor.
type Script struct {
	Input      *checkinputs.ScriptInput
	Fact       checkinputs.Script
	Text       string
	Source     workflowdecl.ScriptSourceView
	Key, Value *workflow.Pos
	Shell      *string
	Config     *config.Config
	Path       string
	Content    []byte
	Scope      *Scope
	Execution  Execution
}
type Batch struct {
	CallGraph checkinputs.CallGraph
	Scopes    []*Scope
	Scripts   []Script
}

// Finding binds a result to the invocation that produced it. Policy is applied
// by the host after adapters finish, including for externally sourced findings.
type Finding struct {
	results.Diagnostic
	Script            int
	Content           []byte
	RawScript         bool
	UnavailableSource findingmodel.UnavailableSource
	Suppression       *Suppression
}
type Suppression struct {
	Path        string
	First, Last int
}
