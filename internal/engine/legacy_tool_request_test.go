package engine

import (
	"context"
	"io"

	repoconfig "actionlint.kjanat.dev/internal/config"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	toolconfig "actionlint.kjanat.dev/internal/config/tools"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

type AnalysisRequest struct {
	Sources    []SourceUnit
	Analyzers  []analyzer.Registration
	ShellCheck string
	// Deprecated: Pyflakes integration was removed. This field is ignored.
	Pyflakes           string
	ShellcheckOptions  *ExternalCommandOptions
	ShellcheckSettings *ShellcheckSettings
	// Deprecated: Pyflakes integration was removed. This field is ignored.
	PyflakesOptions *ExternalCommandOptions
	IgnorePatterns  IgnorePatterns
	// OnRulesCreated customizes workflow rules, as in LinterOptions. It is not called for action metadata.
	OnRulesCreated func([]Rule) []Rule
	// WorkingDir resolves workflow paths in reusable-workflow caches. Empty uses os.Getwd.
	WorkingDir string
	// ReadFile reads local action metadata and reusable workflows. Nil uses os.ReadFile.
	// Custom readers own path resolution and filesystem boundary enforcement.
	// It must support concurrent calls and does not restrict external tools.
	ReadFile          func(string) ([]byte, error)
	RulePresets       RulePresets
	Ruff              string
	RuffOptions       *ExternalCommandOptions
	RuffBackend       analyzer.ScriptBackend
	ShellcheckBackend analyzer.ScriptBackend
	// ShellcheckRunner supplies an isolated embedded ShellCheck backend.
	ShellcheckRunner func([]string, string) ([]byte, error)
	// LoadScriptEngine loads the core WebAssembly engine a configuration selects
	// through tools.<name>.backend.wasm. Nil rejects such configurations.
	LoadScriptEngine func(context.Context, string) (analyzer.ScriptBackend, error)
	// LogWriter receives analysis logs; nil discards them.
	LogWriter io.Writer
	LogLevel  LogLevel
	Processes *analyzerhost.ProcessPool
}

type ShellcheckSettings = toolconfig.ShellcheckSettings
type ShellcheckConfigSelection = toolconfig.ShellcheckConfigSelection
type ShellcheckRCFile = toolconfig.ShellcheckRCFile
type ShellcheckRCMode = toolconfig.ShellcheckRCMode

const (
	ShellcheckRCDisabled = toolconfig.ShellcheckRCDisabled
	ShellcheckRCDiscover = toolconfig.ShellcheckRCDiscover
)

func (r AnalysisRequest) prepared() Request {
	return Request{Sources: r.Sources, Analyzers: r.Analyzers, Tools: adapters.Options{ShellCheck: r.ShellCheck, Ruff: r.Ruff, ShellcheckOptions: r.ShellcheckOptions, RuffOptions: r.RuffOptions, ShellcheckSettings: r.ShellcheckSettings, ShellcheckRunner: r.ShellcheckRunner, ShellcheckBackend: r.ShellcheckBackend, RuffBackend: r.RuffBackend}.Registrations(), IgnorePatterns: r.IgnorePatterns, OnRulesCreated: r.OnRulesCreated, WorkingDir: r.WorkingDir, ReadFile: r.ReadFile, RulePresets: r.RulePresets, LoadScriptEngine: r.LoadScriptEngine, LogWriter: r.LogWriter, LogLevel: r.LogLevel, Processes: r.Processes}
}
func builtinFactory(request AnalysisRequest, logger analysisLogger) func(ruleContext, bool) (builtinAnalyzers, error) {
	return registeredFactory(request.prepared(), logger)
}
func NewScriptEngines(load func(context.Context, string) (analyzer.ScriptBackend, error)) *analyzerhost.ScriptEngines {
	return analyzerhost.NewScriptEngines(load)
}

type ScriptEngines = analyzerhost.ScriptEngines

func (r AnalysisRequest) validateBackends() error {
	for _, t := range r.prepared().Tools {
		if t.Validate != nil {
			if err := t.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

type RuffToolConfig = repoconfig.RuffToolConfig
type ShellcheckConfig = repoconfig.ShellcheckConfig
type ShellcheckConfigSource = repoconfig.ShellcheckConfigSource
type ShellcheckToolConfig = repoconfig.ShellcheckToolConfig

func applyRulePresets(p RulePresets, cfg *Config) *Config { return repoconfig.ResolveRules(p, cfg) }
