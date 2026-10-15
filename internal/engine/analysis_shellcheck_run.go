package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
}

// ShellcheckSettings overrides ShellCheck's workflow-derived defaults.
// A nil Config inherits the project selection; absent project config disables rc discovery.
type ShellcheckSettings struct {
	Config          ShellcheckConfigSelection
	Shell           string
	Exclude         []string
	ExternalSources *bool
}

// ShellcheckConfigSelection selects an rc file, discovery, or disabled rc loading.
type ShellcheckConfigSelection interface{ shellcheckConfigSelection() }

// ShellcheckRCFile selects an explicit rc file.
// Relative application paths use the process working directory. Path
// interpolations use the analyzed project's configuration and action context.
type ShellcheckRCFile string

func (ShellcheckRCFile) shellcheckConfigSelection() {}

// ShellcheckRCMode selects discovery or disables rc loading.
type ShellcheckRCMode bool

func (ShellcheckRCMode) shellcheckConfigSelection() {}

const (
	// ShellcheckRCDisabled disables rc loading, including project-selected paths.
	ShellcheckRCDisabled ShellcheckRCMode = false
	// ShellcheckRCDiscover discovers configuration from the analysis working directory.
	ShellcheckRCDiscover ShellcheckRCMode = true
)

// shellcheckRun owns one prepared script and its callback result. It has no
// visitor lifecycle; its owner must join cmd before reading errs.
type shellcheckRun struct {
	cmd          *externalCommand
	config       *ShellcheckSettings
	cfg          *Config
	paths        runPaths
	rcArgs       []string
	inlineConfig *ShellcheckConfig
	actionPath   string
	ignoreOrigin *diagnosticIgnoreOrigin
	errs         []*Error
	debug        func(string, ...any)
}

func (run *shellcheckRun) Debug(format string, args ...any) {
	if run.debug != nil {
		run.debug(format, args...)
	}
}

func (run *shellcheckRun) addFinding(start, end *Pos, format string, args ...any) {
	finding := errorfAt(start, "shellcheck", format, args...)
	finding.endPosition = end
	if end != nil && end.Line == start.Line && end.Col > start.Col {
		finding.endColumn = end.Col - 1
	}
	run.errs = append(run.errs, finding)
}

// Replace ${{ ... }} with underscores like __________
// Note: replacing with spaces sometimes causes syntax error. For example,
//
//	if ${{ contains(xs, s) }}; then
//	  echo 'hello'
//	fi
func sanitizeExpressionsInScript(src string) string {
	b := strings.Builder{}
	for {
		s := strings.Index(src, "${{")
		if s == -1 {
			if b.Len() == 0 {
				return src
			}
			b.WriteString(src)
			return b.String()
		}

		e := strings.Index(src[s:], "}}")
		if e == -1 {
			if b.Len() == 0 {
				return src
			}
			b.WriteString(src)
			return b.String()
		}
		e += s + 2 // 2 is offset for len("}}")

		b.WriteString(src[:s])
		for _, r := range src[s:e] {
			if r == '\n' || r == '\r' {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}

		src = src[e:]
	}
}

func (run *shellcheckRun) runShellcheck(src string, source *scriptSource, shell shellcheckShell, directory runDirectory, pos, valuePos *Pos) error {
	dialect, setup := shell.analysis()
	header, _, directiveShell := shellcheckHeader(src)
	if dialect == "" && !directiveShell {
		run.Debug("%s: Skip ShellCheck: cannot infer a supported script dialect from shell %q; a leading # shellcheck shell=bash (or another supported dialect) selects it explicitly", pos, shell.name)
		return nil
	}
	if run.rcArgs == nil {
		if err := run.prepareConfigPath(); err != nil {
			return err
		}
	}
	inferred := dialect
	dialectOrigin := "workflow shell"
	// Explicit flags retain native precedence over script directives. Do not append
	// an inferred --shell after them and silently change the requested analysis.
	flagShell, explicitShell := run.cmd.shellcheckDialect()
	appendDialect := !explicitShell && !directiveShell
	if explicitShell {
		dialect = flagShell
		dialectOrigin = "explicit flags"
	} else if directiveShell {
		dialect = shellcheckHeaderDialect(header)
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

	src = sanitizeExpressionsInScript(src)
	run.Debug("%s: Run ShellCheck: shell=%q, dialect=%q (%s), native shell directive=%t, startup=%q:\n%s", pos, shell.name, dialect, dialectOrigin, directiveShell, setup, src)

	// Reasons to exclude the rules:
	//
	// - SC1091: File not found. Scripts are for CI environment. Not suitable for checking this in current local
	//           environment
	// - SC2194: The word is constant. This sometimes happens at constants by replacing ${{ }} with underscores.
	//           For example, `if ${{ matrix.foo }}; then ...` -> `if _________________; then ...`
	// - SC2050: The expression is constant. This sometimes happens at `if` condition by replacing ${{ }} with
	//           underscores (#45). For example, `if [ "${{ matrix.foo }}" = "x" ]` -> `if [ "_________________" = "x" ]`
	// - SC2153: Same as SC2154.
	// - SC2154: The var is referenced but not assigned. Script at `run:` can refer variables defined in `env:` section
	//           so this rule can cause false positives (#53).
	// - SC2157: Argument to -z is always false due to literal strings. When the argument of -z is replaced from ${{ }},
	//           this can happen. For example, `if [ -z ${{ env.FOO }} ]` -> `if [ -z ______________ ]` (#113).
	// - SC2043: Loop can be detected as only running once when the target of iteration is a placeholder. (#355)
	//           e.g. `for foo in ${{ inputs.foo }}; do`
	args := append([]string(nil), run.rcArgs...)
	excluded := []string{"SC1091", "SC2194", "SC2050", "SC2153", "SC2154", "SC2157", "SC2043"}
	if run.config != nil {
		for _, code := range run.config.Exclude {
			if !slices.Contains(excluded, code) {
				excluded = append(excluded, code)
			}
		}
	}
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
	if externalSources {
		args = append(args, "-x")
	}
	args = append(args, "-f", "json1")
	if appendDialect && dialect != "" {
		args = append(args, "--shell", dialect)
	}
	args = append(args, "-e", strings.Join(excluded, ","), "-")
	run.Debug("%s: Running %q with arguments %q", pos, run.cmd.exe, append(slices.Clone(run.cmd.args), args...))
	if options := run.cmd.shellcheckEnvironmentOptions(); options != "" {
		run.Debug("%s: Inherited SHELLCHECK_OPTS=%q", pos, options)
	}

	// Native file-wide directives also work with --norc and never need a temp file.
	prefix, err := inline.directives()
	if err != nil {
		return err
	}
	if !externalSources {
		prefix += "# shellcheck external-sources=false\n"
	}
	if prefix != "" {
		run.Debug("%s: Applied inline ShellCheck directives:\n%s", pos, prefix)
	}
	script := prepareShellcheckScript(src, prefix, setup)
	origin := &suppressionOrigin{first: pos.Line, last: pos.Line}
	if valuePos != nil {
		origin.last = max(origin.last, valuePos.Line)
	}
	if source != nil {
		for _, span := range source.spans {
			origin.last = max(origin.last, span.yamlLine)
		}
	}

	ignoreOrigin := run.ignoreOrigin
	run.cmd.runInDirectory(args, script.text, directory.path, func(stdout []byte, err error) error {
		if err != nil {
			run.Debug("Command %s %s failed: %v", run.cmd.exe, args, err)
			return fmt.Errorf("`%s %s` did not run successfully while checking script at %s: %w", run.cmd.exe, strings.Join(args, " "), pos, err)
		}

		result := shellcheckResult{}
		if err := json.Unmarshal(stdout, &result); err != nil {
			return fmt.Errorf("could not parse JSON output from shellcheck: %w: stdout=%q", err, stdout)
		}
		errs := result.Comments
		if len(errs) == 0 {
			return nil
		}

		first := len(run.errs)
		defer func() {
			for _, finding := range run.errs[first:] {
				finding.ignoreOrigin = ignoreOrigin
			}
		}()
		sources := make(map[string][]byte)
		for _, err := range errs {
			if err.File != "" && err.File != "-" {
				finding := run.sourcedDiagnostic(err, directory.path, sources)
				finding.suppressionOrigin = origin
				run.errs = append(run.errs, finding)
				continue
			}
			// Dialect overrides can make generated startup options non-portable.
			// Their warnings have no workflow source location; errors remain configuration failures.
			line := script.originalLine(err.Line)
			if line == 0 {
				if err.Line != script.startupLine || err.Level == "error" {
					return fmt.Errorf("tools.shellcheck.config: SC%d: %s", err.Code, err.Message)
				}
				continue
			}
			msg := strings.TrimSuffix(err.Message, ".") // Trim period aligning style of error message
			start, startOK := source.pos(line, err.Column)
			end, endOK := source.endPos(script.originalLine(err.EndLine), err.EndColumn)
			if startOK && endOK {
				run.addFinding(start, end, "shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", err.Code, err.Level, line, err.Column, msg)
			} else {
				run.addFinding(pos, nil, "shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", err.Code, err.Level, line, err.Column, msg)
			}
			finding := run.errs[len(run.errs)-1]
			finding.code = fmt.Sprintf("SC%d", err.Code)
			finding.severity = err.Level
			finding.fixes = shellcheckDiagnosticFixes(source, script, err.Fix, err.Message)
		}

		return nil
	})
	return nil
}
