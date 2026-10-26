// Package tools defines script-tool registration independently of tool names and transports.
package tools

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/rule"
)

// Context supplies invocation-owned resources to a registered adapter.
type Context struct {
	Config, ProjectConfig   *config.Config
	WorkingDir, ProjectRoot string
	Processes               *analyzerhost.ProcessPool
	LoadEngine              func(context.Context, string) (analyzer.ScriptBackend, error)
	Debug                   func(string, ...any)
	Warn                    func(error)
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
	Document             analyzer.Input
}

// Registration owns a tool's selection and construction policy. The host only
// consumes capabilities; adding a provider does not change its control flow.
type Registration struct {
	// ToolIntegration identifies a negotiated document provider for this tool.
	ToolIntegration   *analyzer.ToolIntegration
	Descriptor        rule.Descriptor
	ScriptInputs      []analyzer.ScriptInput
	InputSelectors    func(Context) ([]analyzer.ScriptInput, error)
	ConfiguredBackend func(Context) (analyzer.ScriptBackend, error)
	RequiresExecution bool
	NeedsProcess      func(*config.Config, *config.Config, bool) (bool, error)
	Validate          func() error
	Enabled           func(Context) bool
	Required          func(Context) bool
	Build             func(Context) (Runner, error)
	WithBackend       func(analyzer.ScriptBackend) (Registration, error)
	// PrepareInput shares adapter configuration with advertised action-input routes.
	PrepareInput func(Script) (PreparedInput, error)
	// WithAnalyzer adapts a document analyzer to this tool's configuration and policy.
	WithAnalyzer func(analyzer.Analyzer) (Registration, error)
}

// PreparedInput preserves the original scalar's coordinates when an adapter
// inserts configuration directives. A nil OriginalLine leaves rows unchanged.
type PreparedInput struct {
	TemplateContract string
	Source           string
	Config           json.RawMessage
	OriginalLine     func(int) int
	AcceptDiagnostic func(analyzer.ScriptDiagnostic) bool
	Skip             bool
}

type Runner interface {
	Analyze(context.Context, Batch) ([]Finding, error)
}

type Scope struct {
	Config                             *config.Config
	AnalysisDir, Workspace, ActionPath string
	Reader                             *fsread.Reader
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
	Input      *analyzer.ScriptInput
	Fact       analyzer.Script
	Text       string
	Source     documentparse.ScriptSourceView
	Key, Value *workflow.Pos
	Shell      *string
	Config     *config.Config
	Path       string
	Content    []byte
	Scope      *Scope
	Execution  Execution
}
type Batch struct {
	CallGraph analyzer.CallGraph
	Scopes    []*Scope
	Scripts   []Script
}

// Finding binds a result to the invocation that produced it. Policy is applied
// by the host after adapters finish, including for externally sourced findings.
type Finding struct {
	diagnostic.Diagnostic
	Script      int
	Content     []byte
	RawScript   bool
	Suppression *Suppression
}
type Suppression struct {
	Path        string
	First, Last int
}
