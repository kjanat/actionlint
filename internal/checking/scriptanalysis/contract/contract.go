// Package contract defines script-tool registration independently of tool names and transports.
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

type Context struct {
	Config, ProjectConfig   *configuration.Config
	WorkingDir, ProjectRoot string
	Processes               *hostprocess.ProcessPool
	LoadEngine              func(context.Context, string) (scriptengine.Backend, error)
	Debug                   func(string, ...any)
	Warn                    func(error)
	// Providers may call Skipped and Completed concurrently.
	Skipped   func(string)
	Completed func(string)
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
	Document             checkinputs.AnalyzerInput
}

type Registration struct {
	ToolSettings          *toolsettings.Spec
	InvocationDefinitions []commands.Definition
	ToolIntegration       *checkinputs.ToolIntegration
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
	PrepareInput      func(context.Context, scriptanalysis.Unit) (preparation.Prepared, error)
	// The registration's options replace the configured rule options in WithAnalyzer.
	WithAnalyzer func(results.AnalyzerRegistration) (Registration, error)
}

// Runner returns findings whose Script indexes batch.Scripts.
type Runner interface {
	Analyze(context.Context, scriptanalysis.Batch) ([]ToolFinding, error)
}

// ToolFinding receives policy from the host after adapters finish.
type ToolFinding struct {
	results.Diagnostic
	Script int
	// Source.Suppression stays empty for policy to fill in.
	Source      results.DiagnosticSource
	Suppression *results.SuppressionOrigin
}
