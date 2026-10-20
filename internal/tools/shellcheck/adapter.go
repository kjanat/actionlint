package shellcheck

import (
	"context"
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/config/rules"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/selection"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

type Config = config.Config
type ShellcheckConfig = config.ShellcheckConfig
type ExternalCommandOptions = processconfig.Options
type externalCommand = analyzerhost.Command
type DiagnosticPosition = diagnostic.Position
type DiagnosticFix = diagnostic.Fix
type DiagnosticEdit = diagnostic.Edit

type Error struct {
	diagnostic.Diagnostic
	endPosition       *workflowlang.Pos
	endColumn         int
	source            []byte
	rawScriptSource   bool
	scriptIndex       int
	suppressionOrigin *suppressionOrigin
}
type suppressionOrigin struct {
	path        string
	first, last int
}

func errorfAt(pos *workflowlang.Pos, name, format string, args ...any) *Error {
	return &Error{Rule: name, Message: fmt.Sprintf(format, args...), Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}}
}
func absPath(path string) string {
	p, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return p
}
func diagnosticLevel(cfg *Config, name string) rules.Level {
	return config.DiagnosticLevel(cfg, name)
}

type runPaths struct {
	analysis, workspace string
	reader              *fsread.Reader
}
type directoryKind uint8

const (
	directoryUnspecified directoryKind = iota
	directoryKnown
	directoryUnknown
)

type runDirectory struct {
	kind directoryKind
	path string
}
type scriptScope struct {
	paths      runPaths
	config     *Config
	actionPath string
	composite  bool
}
type scriptExecution struct {
	shell     shellcheckShell
	directory runDirectory
}
type scriptUnit struct {
	run       *workflowlang.ExecRun
	shell     *string
	config    *Config
	path      string
	content   []byte
	scope     *scriptScope
	execution scriptExecution
}
type scriptBatch struct {
	scopes  []*scriptScope
	scripts []scriptUnit
}
type scriptAnalyzer interface {
	AnalyzeScripts(context.Context, scriptBatch) ([]*Error, error)
}
type adapterContext struct {
	config             *Config
	workingDir         string
	process            *analyzerhost.ProcessPool
	debug              func(string, ...any)
	scriptEngine       func(string) (analyzer.ScriptBackend, error)
	shellcheck         string
	shellcheckOptions  *ExternalCommandOptions
	shellcheckBackend  analyzer.ScriptBackend
	shellcheckRunner   func([]string, string) ([]byte, error)
	shellcheckSettings *ShellcheckSettings
}

func (c adapterContext) configuredScriptEngine(name, requested string, explicit *ExternalCommandOptions) (analyzer.ScriptBackend, error) {
	return selection.Load(name, requested, explicit, c.config, c.scriptEngine)
}

type runner struct{ check scriptAnalyzer }

func (r runner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	private := scriptBatch{}
	scopes := map[*tools.Scope]*scriptScope{}
	for _, scope := range batch.Scopes {
		s := &scriptScope{paths: runPaths{analysis: scope.AnalysisDir, workspace: scope.Workspace, reader: scope.Reader}, config: scope.Config, actionPath: scope.ActionPath, composite: scope.Composite}
		scopes[scope] = s
		private.scopes = append(private.scopes, s)
	}
	for _, s := range batch.Scripts {
		kind := directoryUnknown
		if s.Execution.DirectoryKnown {
			kind = directoryKnown
		}
		private.scripts = append(private.scripts, scriptUnit{run: &workflowlang.ExecRun{Run: &workflowlang.String{Value: s.Text, Pos: s.Value}, Source: s.Source, RunPos: s.Key}, shell: s.Shell, config: s.Config, path: s.Path, content: s.Content, scope: scopes[s.Scope], execution: scriptExecution{shell: shellcheckShell{name: s.Execution.Shell, implicit: s.Execution.ImplicitShell}, directory: runDirectory{kind: kind, path: s.Execution.Directory}}})
	}
	findings, err := r.check.AnalyzeScripts(ctx, private)
	result := make([]tools.Finding, 0, len(findings))
	for _, f := range findings {
		d := f.Diagnostic
		if f.endPosition != nil {
			d.End = diagnostic.Position{Line: f.endPosition.Line, Column: f.endPosition.Col}
		}
		out := tools.Finding{Diagnostic: d, Script: f.scriptIndex, Content: f.source, RawScript: f.rawScriptSource}
		if o := f.suppressionOrigin; o != nil {
			out.Suppression = &tools.Suppression{Path: o.path, First: o.first, Last: o.last}
		}
		result = append(result, out)
	}
	return result, err
}
