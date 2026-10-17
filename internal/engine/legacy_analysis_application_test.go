package engine

import (
	"context"
	"io"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/workspace"
)

// AnalysisOptions controls source discovery and analysis without selecting an output format.
type AnalysisOptions struct {
	Context       context.Context
	Analyzers     []analyzer.Registration
	WorkingDir    string
	StdinFileName string
	ConfigFile    string
	// ConfigOverlays apply in order over the selected file.
	ConfigOverlays []ConfigOverlay
	// OnConfigLoaded reports selected files and overlays.
	OnConfigLoaded func(ConfigReport)
	// SkipProjectConfig disables per-project config reads; ConfigFile still applies.
	SkipProjectConfig bool
	// QuietSelection suppresses the legacy file-selection and completion log messages.
	QuietSelection bool
	Shellcheck     string
	// Deprecated: Pyflakes integration was removed. This field is ignored.
	Pyflakes           string
	ShellcheckOptions  *ExternalCommandOptions
	ShellcheckSettings *ShellcheckSettings
	// Deprecated: Pyflakes integration was removed. This field is ignored.
	PyflakesOptions *ExternalCommandOptions
	IgnorePatterns  []string
	Verbose         bool
	Debug           bool
	LogWriter       io.Writer
	// OnRulesCreated customizes workflow rules, as in LinterOptions. It is not called for action metadata.
	OnRulesCreated  func([]Rule) []Rule
	OnFilesSelected func([]string)
	// ReadFile reads workflows, local action metadata, reusable workflows, and config.
	// It must support concurrent calls. Nil uses os.ReadFile.
	// Custom readers own path resolution and filesystem boundary enforcement.
	// This does not restrict filesystem access by external tools.
	ReadFile          func(string) ([]byte, error)
	RulePresets       RulePresets
	Ruff              string
	RuffOptions       *ExternalCommandOptions
	RuffBackend       analyzer.ScriptBackend
	ShellcheckBackend analyzer.ScriptBackend
	// ShellcheckRunner supplies an isolated embedded ShellCheck backend.
	ShellcheckRunner func([]string, string) ([]byte, error)
	// LoadScriptEngine loads the core WebAssembly engine a configuration selects
	// through tools.<name>.backend.wasm. The session loads each path once and
	// releases the engines in Close. Nil rejects such configurations.
	LoadScriptEngine func(context.Context, string) (analyzer.ScriptBackend, error)
}

// AnalysisSession adapts the legacy session API to independent preparation and analysis.
// New application callers prepare inputs with workspace and invoke Analyze directly.
type AnalysisSession struct {
	*workspace.Session
	analysisLogger
	engines      *ScriptEngines
	request      AnalysisRequest
	ctx          context.Context
	logSelection bool
}

func NewAnalysisSession(opts AnalysisOptions) (*AnalysisSession, error) {
	a := &AnalysisSession{ctx: opts.Context, logSelection: !opts.QuietSelection,
		request: AnalysisRequest{Analyzers: opts.Analyzers, Ruff: opts.Ruff, RuffOptions: opts.RuffOptions, RuffBackend: opts.RuffBackend, ShellcheckBackend: opts.ShellcheckBackend, ShellCheck: opts.Shellcheck, ShellcheckRunner: opts.ShellcheckRunner, ShellcheckOptions: opts.ShellcheckOptions, ShellcheckSettings: opts.ShellcheckSettings, OnRulesCreated: opts.OnRulesCreated, ReadFile: opts.ReadFile, RulePresets: opts.RulePresets},
		logOut:  opts.LogWriter,
	}
	if err := a.request.validateBackends(); err != nil {
		return nil, err
	}
	if a.ctx == nil {
		a.ctx = context.Background()
	}
	if a.logOut == nil {
		a.logOut = io.Discard
	}
	if opts.Verbose {
		a.logLevel = LogLevelVerbose
	} else if opts.Debug {
		a.logLevel = LogLevelDebug
	}
	var err error
	a.request.IgnorePatterns, err = CompileIgnorePatterns(opts.IgnorePatterns)
	if err != nil {
		return nil, err
	}
	a.Session, err = workspace.New(workspace.Options{Context: a.ctx, WorkingDir: opts.WorkingDir, StdinFileName: opts.StdinFileName, ConfigFile: opts.ConfigFile, ConfigOverlays: opts.ConfigOverlays, OnConfigLoaded: opts.OnConfigLoaded, SkipProjectConfig: opts.SkipProjectConfig, OnFilesSelected: opts.OnFilesSelected, ReadFile: opts.ReadFile, RuleNames: registeredRuleNames(opts.Analyzers), Log: a.selectionLog, LogWriter: a.logOut})
	if err != nil {
		return nil, err
	}
	a.request.WorkingDir = a.WorkingDir()
	if opts.LoadScriptEngine != nil {
		a.engines = NewScriptEngines(opts.LoadScriptEngine)
		a.request.LoadScriptEngine = a.engines.Load
	}
	return a, nil
}
func (a *AnalysisSession) selectionLog(args ...any) {
	if a.logSelection {
		a.log(args...)
	}
}
func (a *AnalysisSession) Repository(dir string) (*AnalysisResult, error) {
	return a.run(a.Session.Repository(dir))
}
func (a *AnalysisSession) directory(dir string, project *Project) (*AnalysisResult, error) {
	return a.run(a.Directory(dir, project))
}
func (a *AnalysisSession) Files(paths []string, project *Project) (*AnalysisResult, error) {
	return a.run(a.Session.Files(paths, project))
}
func (a *AnalysisSession) Empty() (*AnalysisResult, error) { return a.run(a.Session.Empty()) }
func (a *AnalysisSession) readFiles(paths []string, project *Project) (*AnalysisResult, error) {
	return a.run(a.ReadFiles(paths, project))
}
func (a *AnalysisSession) ReadStdin(reader io.Reader, normalize bool) (*AnalysisResult, error) {
	return a.run(a.Session.ReadStdin(reader, normalize))
}
func (a *AnalysisSession) content(path string, content []byte, project *Project, normalize bool) (*AnalysisResult, error) {
	return a.run(a.Content(path, content, project, normalize))
}
func (a *AnalysisSession) configForProject(project *Project) (*Config, error) {
	return a.ConfigForProject(project)
}
func (a *AnalysisSession) run(selection *workspace.Selection, err error) (*AnalysisResult, error) {
	if err != nil {
		return nil, err
	}
	if len(selection.Sources) == 0 {
		return &AnalysisResult{Diagnostics: []Diagnostic{}, Inputs: selection.Inputs, Configurations: selection.Configurations}, nil
	}
	request := a.request
	request.Sources = selection.Sources
	result, err := analyze(a.ctx, request, a.logOut, a.logLevel)
	if result != nil {
		result.Configurations = selection.Configurations
		result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, selection.Inputs...))))
	}
	return result, err
}

// Completed writes the selection summary after successful rendering.
func (a *AnalysisSession) Completed(result *AnalysisResult) {
	if result.FileCount > 1 {
		a.selectionLog("Found", len(result.Diagnostics), "errors in", result.FileCount, "files")
	}
}
func (a *AnalysisSession) Close(ctx context.Context) error { return a.engines.Close(ctx) }
