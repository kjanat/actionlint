package actionlint

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type shellcheckError struct {
	Line      int    `json:"line"`
	EndLine   int    `json:"endLine"`
	Column    int    `json:"column"`
	EndColumn int    `json:"endColumn"`
	Level     string `json:"level"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
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

// RuleShellcheck is a rule to check shell scripts at 'run:' using shellcheck.
// https://github.com/koalaman/shellcheck
type RuleShellcheck struct {
	RuleBase
	cmd           *externalCommand
	config        *ShellcheckSettings
	workflowShell shellValue
	jobShell      shellValue
	runnerShell   shellValue
	workflowDir   runDirectory
	jobDir        runDirectory
	paths         runPaths
	rcArgs        []string
	onInput       func(string)
	mu            sync.Mutex
}

func newRuleShellcheck(cmd *externalCommand) *RuleShellcheck {
	return &RuleShellcheck{
		RuleBase: builtinRuleBase("shellcheck"),
		cmd:      cmd,
	}
}

// NewRuleShellcheck creates new RuleShellcheck instance. The executable argument can be command
// name or relative/absolute file path. When the given executable is not found in system, it returns
// an error as 2nd return value.
func NewRuleShellcheck(executable string, proc *concurrentProcess) (*RuleShellcheck, error) {
	return configuredShellcheck(executable, nil, nil, proc)
}

func configuredShellcheck(executable string, options *ExternalCommandOptions, config *ShellcheckSettings, proc *concurrentProcess) (*RuleShellcheck, error) {
	cmd, err := proc.configuredCommandRunner(executable, options, false)
	if err != nil {
		return nil, err
	}
	rule := newRuleShellcheck(cmd)
	rule.config = config
	return rule, nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleShellcheck) VisitStep(n *Step) error {
	run, ok := n.Exec.(*ExecRun)
	if !ok || run.Run == nil {
		return nil
	}

	return rule.runShellcheck(run.Run.Value, run.source, rule.resolveShell(run), rule.paths.resolve(effectiveRunDirectory(run, rule.jobDir, rule.workflowDir)), run.RunPos)
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleShellcheck) VisitJobPre(n *Job) error {
	rule.jobShell = defaultsShellValue(n.Defaults)
	rule.jobDir = defaultsWorkingDirectory(n.Defaults)
	rule.runnerShell = shellValue{}
	if runnerPlatform(n.RunsOn) == platformKindWindows {
		rule.runnerShell = shellValueFromString(&String{Value: "pwsh"})
	}
	if container := shellcheckContainerShell(n.Container); container.kind != shellValueUnspecified {
		rule.runnerShell = container
	}

	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children.
func (rule *RuleShellcheck) VisitJobPost(n *Job) error {
	rule.jobShell = shellValue{}
	rule.jobDir = runDirectory{}
	rule.runnerShell = shellValue{}
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleShellcheck) VisitWorkflowPre(n *Workflow) error {
	rule.workflowShell = defaultsShellValue(n.Defaults)
	rule.workflowDir = defaultsWorkingDirectory(n.Defaults)
	return rule.prepareConfigPath()
}

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children.
func (rule *RuleShellcheck) VisitWorkflowPost(n *Workflow) error {
	rule.workflowShell = shellValue{}
	rule.workflowDir = runDirectory{}
	return rule.cmd.wait() // Wait until all processes running for this rule
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

func (rule *RuleShellcheck) runShellcheck(src string, source *scriptSource, shell shellcheckShell, directory runDirectory, pos *Pos) error {
	dialect, setup := shell.analysis()
	if dialect == "" {
		return nil // Skip checking this shell script since shellcheck doesn't support it
	}
	var inline *ShellcheckConfig
	if config := rule.Config(); config != nil {
		inline = config.Tools.Shellcheck.Config.inline()
	}
	if inline != nil && inline.Shell != nil {
		dialect = *inline.Shell
	}
	if rule.config != nil && rule.config.Shell != "" {
		dialect = rule.config.Shell
	}

	src = sanitizeExpressionsInScript(src)
	rule.Debug("%s: Run shellcheck for %s script:\n%s", pos, dialect, src)

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
	args := append([]string(nil), rule.rcArgs...)
	excluded := []string{"SC1091", "SC2194", "SC2050", "SC2153", "SC2154", "SC2157", "SC2043"}
	if rule.config != nil {
		for _, code := range rule.config.Exclude {
			if !slices.Contains(excluded, code) {
				excluded = append(excluded, code)
			}
		}
	}
	externalSources := true
	if inline != nil && inline.ExternalSources != nil {
		externalSources = *inline.ExternalSources
	}
	if rule.config != nil && rule.config.ExternalSources != nil {
		externalSources = *rule.config.ExternalSources
	}
	if directory.kind == directoryUnknown {
		externalSources = false
	}
	if externalSources {
		args = append(args, "-x")
	}
	args = append(args, "-f", "json1", "--shell", dialect, "-e", strings.Join(excluded, ","), "-")
	rule.Debug("%s: Running %s command with %s", pos, rule.cmd.exe, args)

	// Native file-wide directives also work with --norc and never need a temp file.
	prefix, err := inline.directives()
	if err != nil {
		return err
	}
	if !externalSources {
		prefix += "# shellcheck external-sources=false\n"
	}
	prefix += setup + "\n"
	prefixLines := strings.Count(prefix, "\n")
	script := prefix + src + "\n"

	rule.cmd.runInDirectory(args, script, directory.path, func(stdout []byte, err error) error {
		if err != nil {
			rule.Debug("Command %s %s failed: %v", rule.cmd.exe, args, err)
			return fmt.Errorf("`%s %s` did not run successfully while checking script at %s: %w", rule.cmd.exe, strings.Join(args, " "), pos, err)
		}

		result := shellcheckResult{}
		if err := json.Unmarshal(stdout, &result); err != nil {
			return fmt.Errorf("could not parse JSON output from shellcheck: %w: stdout=%q", err, stdout)
		}
		errs := result.Comments
		if len(errs) == 0 {
			return nil
		}

		// Synchronize rule.Errorf calls
		rule.mu.Lock()
		defer rule.mu.Unlock()
		for _, err := range errs {
			// Startup options are synthetic, not workflow source. A dialect override
			// can make those options non-portable without creating a user-code finding.
			if err.Line < prefixLines || err.Line == prefixLines && err.Level == "error" {
				return fmt.Errorf("tools.shellcheck.config: SC%d: %s", err.Code, err.Message)
			}
			if err.Line == prefixLines {
				continue
			}
			// Consider the first line is setup for running shell which was implicitly added for better check
			line := err.Line - prefixLines
			msg := strings.TrimSuffix(err.Message, ".") // Trim period aligning style of error message
			if start, ok := source.pos(line, err.Column); ok {
				end, _ := source.endPos(err.EndLine-prefixLines, err.EndColumn)
				rule.errorfRange(start, end, "shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", err.Code, err.Level, line, err.Column, msg)
				continue
			}
			rule.Errorf(pos, "shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", err.Code, err.Level, line, err.Column, msg)
		}

		return nil
	})
	return nil
}
