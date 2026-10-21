// Package tools defines script-tool registration independently of tool names and transports.
package tools

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/rule"
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
}

// Registration owns a tool's selection and construction policy. The host only
// consumes capabilities; adding a provider does not change its control flow.
type Registration struct {
	Descriptor        rule.Descriptor
	ScriptInputs      []analyzer.ScriptInput
	InputSelectors    func(Context) ([]analyzer.ScriptInput, error)
	RequiresExecution bool
	NeedsProcess      func(*config.Config, *config.Config, bool) (bool, error)
	Validate          func() error
	Enabled           func(Context) bool
	Required          func(Context) bool
	Build             func(Context) (Runner, error)
	WithBackend       func(analyzer.ScriptBackend) (Registration, error)
	// PrepareInput shares adapter configuration with advertised action-input routes.
	PrepareInput func(Script) (PreparedInput, error)
}

// PreparedInput preserves the original scalar's coordinates when an adapter
// inserts configuration directives. A nil OriginalLine leaves rows unchanged.
type PreparedInput struct {
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
	Text       string
	Source     *workflow.ScriptSource
	Key, Value *workflow.Pos
	Shell      *string
	Config     *config.Config
	Path       string
	Content    []byte
	Scope      *Scope
	Execution  Execution
}
type Batch struct {
	Scopes  []*Scope
	Scripts []Script
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
