package shellcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	"actionlint.kjanat.dev/v2/internal/tools/profile/mask"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

type shellcheckError struct {
	File      string         `json:"file"`
	Line      int            `json:"line"`
	EndLine   int            `json:"endLine"`
	Column    int            `json:"column"`
	EndColumn int            `json:"endColumn"`
	Level     string         `json:"level"`
	Code      int            `json:"code"`
	Message   string         `json:"message"`
	Fix       *shellcheckFix `json:"fix"`
}

type shellcheckResult struct {
	Comments []shellcheckError `json:"comments"`
	Sources  []string          `json:"sources"`
}

// shellcheckRun owns one prepared script and its callback result. It has no
// visitor lifecycle; its owner must join cmd before reading errs.
type shellcheckRun struct {
	backend      analyzer.ScriptBackend
	pending      analyzerhost.Task[*Error]
	cmd          *externalCommand
	config       *shellprofile.Settings
	cfg          *Config
	paths        runPaths
	rcArgs       []string
	inlineConfig *ShellcheckConfig
	actionPath   string
	scriptIndex  int
	errs         []*Error
	debug        func(string, ...any)
}

func (run *shellcheckRun) Debug(format string, args ...any) {
	if run.debug != nil {
		run.debug(format, args...)
	}
}

func (run *shellcheckRun) addFinding(start, end *workflowlang.Pos, format string, args ...any) {
	finding := errorfAt(start, "shellcheck", format, args...)
	finding.endPosition = end
	if end != nil && end.Line == start.Line && end.Col > start.Col {
		finding.endColumn = end.Col - 1
	}
	run.errs = append(run.errs, finding)
}

func (run *shellcheckRun) runShellcheck(src string, source workflowdecl.ScriptSourceView, shell shellprofile.Shell, directory runDirectory, pos, valuePos *workflowlang.Pos) error {
	dialect, setup := shell.Analysis()
	header, _, directiveShell := shellprofile.Header(src)
	if dialect == "" && !directiveShell {
		run.Debug("%s: Skip ShellCheck: cannot infer a supported script dialect from shell %q; a leading # shellcheck shell=bash (or another supported dialect) selects it explicitly", pos, shell.Name)
		return nil
	}
	if run.rcArgs == nil {
		if err := run.prepareConfigPath(); err != nil {
			return err
		}
	}
	inferred := dialect
	dialectOrigin := "workflow shell"
	// An explicit --shell takes precedence over directives and inferred dialects.
	var flagShell string
	explicitShell := false
	if run.cmd != nil {
		flagShell, explicitShell = shellcheckDialect(run.cmd)
	}
	appendDialect := !explicitShell && !directiveShell
	if explicitShell {
		dialect = flagShell
		dialectOrigin = "explicit flags"
	} else if directiveShell {
		dialect = shellprofile.HeaderDialect(header)
		dialectOrigin = "script directive"
	}
	inline := run.inlineConfig
	if appendDialect {
		if inline != nil && inline.Shell != nil {
			dialect = *inline.Shell
			dialectOrigin = "inline configuration"
		}
		if run.config != nil && run.config.Shell != "" {
			dialect = run.config.Shell
			dialectOrigin = "application settings"
		}
	}
	if dialect != inferred {
		setup = "" // Runtime options may not exist in an explicitly selected dialect.
	}

	src = mask.Expressions(src)
	run.Debug("%s: Run ShellCheck: shell=%q, dialect=%q (%s), native shell directive=%t, startup=%q:\n%s", pos, shell.Name, dialect, dialectOrigin, directiveShell, setup, src)

	// The shell profile excludes checks masked templates make false positives.
	args := append([]string(nil), run.rcArgs...)
	excluded := excludedChecks(run.config)
	externalSources := true
	if inline != nil && inline.ExternalSources != nil {
		externalSources = *inline.ExternalSources
	}
	if run.config != nil && run.config.ExternalSources != nil {
		externalSources = *run.config.ExternalSources
	}
	if directory.kind == directoryUnknown {
		externalSources = false
		run.Debug("%s: Source following disabled: working directory cannot be resolved locally; analysis directory=%q", pos, directory.path)
	} else {
		run.Debug("%s: Working directory=%q; source following requested=%t", pos, directory.path, externalSources)
	}
	if run.backend != nil {
		externalSources = false
	}
	if externalSources {
		args = append(args, "-x")
	}
	args = append(args, "-f", "json1")
	if appendDialect && dialect != "" {
		args = append(args, "--shell", dialect)
	}
	args = append(args, "-e", strings.Join(excluded, ","), "-")
	if run.cmd != nil {
		run.Debug("%s: Running %q with arguments %q", pos, run.cmd.Exe, append(slices.Clone(run.cmd.Args), args...))
		if options := shellcheckEnvironmentOptions(run.cmd); options != "" {
			run.Debug("%s: Inherited SHELLCHECK_OPTS=%q", pos, options)
		}
	}
	// Native file-wide directives also work with --norc and never need a temp file.
	prefix, err := inline.Directives()
	if err != nil {
		return err
	}
	if !externalSources {
		prefix += "# shellcheck external-sources=false\n"
	}
	if prefix != "" {
		run.Debug("%s: Applied inline ShellCheck directives:\n%s", pos, prefix)
	}
	script := shellprofile.Prepare(src, prefix, setup)
	origin := &suppressionOrigin{first: pos.Line, last: pos.Line}
	if valuePos != nil {
		origin.last = max(origin.last, valuePos.Line)
	}
	if source.Present() {
		for span := range source.Spans() {
			origin.last = max(origin.last, span.YamlLine)
		}
	}

	ignoreOrigin := run.scriptIndex
	deliver := func(errs []shellcheckError) error {
		first := len(run.errs)
		defer func() {
			for _, finding := range run.errs[first:] {
				finding.scriptIndex = ignoreOrigin
			}
		}()
		sources := make(map[string]reportedSource)
		for _, err := range errs {
			if err.File != "" && err.File != "-" {
				finding := run.sourcedDiagnostic(err, directory.path, sources)
				finding.suppressionOrigin = origin
				run.errs = append(run.errs, finding)
				continue
			}
			prepared := script.Line(err.Line)
			switch prepared.Origin {
			case profile.LineScript:
			case profile.LineStartup:
				// Dialect overrides can make generated startup options non-portable.
				if err.Level != "error" {
					continue
				}
				return fmt.Errorf("tools.shellcheck.config: SC%d: %s", err.Code, err.Message)
			case profile.LineDirective:
				return fmt.Errorf("tools.shellcheck.config: SC%d: %s", err.Code, err.Message)
			}
			line := prepared.Line
			msg := strings.TrimSuffix(err.Message, ".") // Trim period aligning style of error message
			start, startOK := source.Position(line, err.Column)
			end, endOK := source.EndPos(script.OriginalLine(err.EndLine), err.EndColumn)
			if startOK && endOK {
				run.addFinding(&start, &end, "shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", err.Code, err.Level, line, err.Column, msg)
			} else {
				run.addFinding(pos, nil, "shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", err.Code, err.Level, line, err.Column, msg)
			}
			finding := run.errs[len(run.errs)-1]
			finding.Code = fmt.Sprintf("SC%d", err.Code)
			finding.Severity = err.Level
			finding.Fixes = shellcheckDiagnosticFixes(source, script, err.Fix, err.Message)
		}

		return nil
	}
	if run.backend != nil {
		options, err := backendOptions(dialect, run.config)
		if err != nil {
			return err
		}
		request := analyzer.ScriptRequest{Source: script.Text(), Filename: "actionlint.sh", Config: options}
		run.pending = func(ctx context.Context) ([]*Error, error) {
			if validator, ok := run.backend.(analyzer.ScriptConfigValidator); ok {
				if err := validator.ValidateConfig(ctx, options); err != nil {
					return nil, err
				}
			}
			findings, failure := run.backend.Check(ctx, request)
			converted := make([]shellcheckError, len(findings))
			for i, finding := range findings {
				code, err := strconv.Atoi(strings.TrimPrefix(finding.Code, "SC"))
				if err != nil || !strings.HasPrefix(finding.Code, "SC") || finding.Filename != request.Filename || finding.Message == "" || finding.Location.Row < 1 || finding.Location.Column < 1 {
					return nil, errors.Join(failure, fmt.Errorf("invalid ShellCheck backend diagnostic: %+v", finding))
				}
				converted[i] = shellcheckError{Code: code, Message: finding.Message, Level: finding.Severity, Line: finding.Location.Row, Column: finding.Location.Column, EndLine: finding.EndLocation.Row, EndColumn: finding.EndLocation.Column}
			}
			return nil, errors.Join(failure, deliver(converted))
		}
	} else {
		run.cmd.RunInDirectory(args, script.Text(), directory.path, func(stdout []byte, err error) error {
			if err != nil {
				return fmt.Errorf("`%s %s` did not run successfully while checking script at %s: %w", run.cmd.Exe, strings.Join(args, " "), pos, err)
			}
			var result shellcheckResult
			if err := json.Unmarshal(stdout, &result); err != nil {
				return fmt.Errorf("could not parse JSON output from shellcheck: %w: stdout=%q", err, stdout)
			}
			return errors.Join(run.recordSources(result.Sources, directory.path), deliver(result.Comments))
		})
	}
	return nil
}
