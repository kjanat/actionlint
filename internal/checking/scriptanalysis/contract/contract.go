// Package contract defines script-tool registration independently of tool names and transports.
// Tools analyze the scripts.Batch the host prepares for them.
package contract

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Context supplies invocation-owned resources to a registered adapter.
type Context struct {
	Config, ProjectConfig   *configuration.Config
	WorkingDir, ProjectRoot string
	Processes               *hostprocess.ProcessPool
	LoadEngine              func(context.Context, string) (scriptengine.Backend, error)
	Debug                   func(string, ...any)
	Warn                    func(error)
	// Skipped receives the rule name of an enabled check that could not run.
	// Providers may call it concurrently; disabling a check does not call it.
	Skipped func(string)
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
	Document             checkinputs.AnalyzerInput
}

// Registration owns a tool's selection and construction policy. The host only
// consumes capabilities; adding a provider does not change its control flow.
type Registration struct {
	ToolSettings          *toolsettings.Spec
	InvocationDefinitions []commands.Definition
	// ToolIntegration identifies a negotiated document provider for this tool.
	ToolIntegration *checkinputs.ToolIntegration
	rule.Declaration
	ScriptInputs      []checkinputs.ScriptInput
	InputSelectors    func(Context) ([]checkinputs.ScriptInput, error)
	ConfiguredBackend func(Context) (scriptengine.Backend, error)
	RequiresExecution bool
	NeedsProcess      func(context.Context, *configuration.Config, *configuration.Config) (bool, error)
	Validate          func() error
	Enabled           func(Context) bool
	Required          func(Context) bool
	Build             func(Context) (Runner, error)
	WithBackend       func(scriptengine.Backend) (Registration, error)
	// PrepareInput shares adapter configuration with advertised action-input routes.
	PrepareInput func(context.Context, scriptanalysis.Unit) (preparation.Prepared, error)
	// WithAnalyzer adapts a document analyzer to this tool's configuration and
	// policy. The registration's options replace the configured rule options.
	WithAnalyzer func(checkinputs.Registration) (Registration, error)
}

// Runner analyzes the batch prepared for one tool. ToolFinding.Script indexes
// batch.Scripts.
type Runner interface {
	Analyze(context.Context, scriptanalysis.Batch) ([]ToolFinding, error)
}

// ToolFinding binds a result to the invocation that produced it. Policy is applied
// by the host after adapters finish, including for externally sourced findings.
type ToolFinding struct {
	results.Diagnostic
	Script int
	// Source is the text of the reported file. Source.Suppression is left for
	// policy to fill in.
	Source results.DiagnosticSource
	// Suppression identifies the YAML run declaration which caused the finding.
	Suppression *results.SuppressionOrigin
}
