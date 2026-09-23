package actionlint

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

// RuleExecutableBit checks direct script calls against the Git index, independently
// of host filesystem permissions and of ShellCheck availability.
type RuleExecutableBit struct {
	RuleBase
	context                    ruleContext
	workflowDir, jobDir        runDirectory
	workflowShell, jobShell    shellValue
	paths                      runPaths
	unix, sequential, pristine bool
	changed                    map[string]bool
	workflowEnv, jobEnv        bool
	skipFindings               bool
	repositoryUnknown          bool
}

func newRuleExecutableBit(context ruleContext) *RuleExecutableBit {
	return &RuleExecutableBit{RuleBase: builtinRuleBase("executable-bit"), context: context}
}

func (rule *RuleExecutableBit) VisitWorkflowPre(workflow *Workflow) error {
	rule.workflowDir = defaultsWorkingDirectory(workflow.Defaults)
	rule.workflowShell = defaultsShellValue(workflow.Defaults)
	rule.workflowEnv = shellEnvironmentUnknown(workflow.Env)
	return nil
}

func (rule *RuleExecutableBit) VisitJobPre(job *Job) error {
	rule.jobDir, rule.jobShell = defaultsWorkingDirectory(job.Defaults), defaultsShellValue(job.Defaults)
	rule.jobEnv = rule.workflowEnv || shellEnvironmentUnknown(job.Env)
	rule.unix = runnerPlatform(job.RunsOn) == platformKindMacOrLinux
	// Container mounts can replace the checked-out tree. Treat them as unknown.
	if job.Container != nil {
		rule.unix = false
	}
	if enabled, known := stepCondition(job.If); known && !enabled {
		rule.unix = false
	}
	rule.sequential, rule.pristine = true, false
	rule.repositoryUnknown = false
	rule.changed = make(map[string]bool)
	rule.paths = runPaths{workspace: rule.context.projectRoot, analysis: rule.context.workingDir}
	return nil
}

func (rule *RuleExecutableBit) VisitStep(step *Step) error {
	if !rule.unix || !rule.sequential {
		return nil
	}
	enabled, conditionKnown := stepCondition(step.If)
	if conditionKnown && !enabled {
		return nil
	}
	if boolMayBeTrue(step.Background) {
		rule.sequential, rule.pristine = false, false
		return nil
	}
	switch command := step.Exec.(type) {
	case *ExecParallel:
		rule.sequential, rule.pristine = false, false
	case *ExecAction:
		rule.checkout(command, !conditionKnown || boolMayBeTrue(step.ContinueOnError))
	case *ExecRun:
		if rule.jobEnv || shellEnvironmentUnknown(step.Env) {
			rule.pristine = false
		}
		if rule.pristine {
			rule.checkScript(command)
		}
		if !conditionKnown {
			// Its invocation sees the current state, but its effects may be skipped.
			rule.pristine = false
		}
		if !rule.pristine {
			rule.repositoryUnknown = true
		}
	}
	return nil
}

func stepCondition(condition *String) (enabled, known bool) {
	if condition == nil {
		return true, true
	}
	expression := *condition
	if !expression.ContainsExpression() {
		expression.Value = "${{ " + expression.Value + " }}"
	}
	value, known := workflowExpressionLiteral(&expression)
	enabled, boolean := value.(bool)
	return enabled, known && boolean
}

func boolMayBeTrue(value *Bool) bool {
	if value == nil {
		return false
	}
	if value.Expression == nil {
		return value.Value
	}
	literal, known := workflowExpressionLiteral(value.Expression)
	enabled, boolean := literal.(bool)
	return !known || !boolean || enabled
}

// A known self checkout establishes which index is represented in the workspace.
// Opaque actions may change permissions or replace files, so invalidate that state.
func (rule *RuleExecutableBit) checkout(action *ExecAction, mayNotComplete bool) {
	rule.pristine = false
	if rule.repositoryUnknown {
		return
	}
	// Opaque execution can change Git settings that preserve working-tree modes.
	rule.repositoryUnknown = true
	if action.Uses == nil || mayNotComplete || action.InputsExpression != nil {
		return
	}
	name, _, versioned := strings.Cut(action.Uses.Value, "@")
	if !versioned || !strings.EqualFold(name, "actions/checkout") {
		return
	}
	for _, input := range []string{"repository", "ref", "sparse-checkout"} {
		if value := action.Inputs[input]; value != nil && value.Value != nil && value.Value.Value != "" {
			return
		}
	}
	if input := action.Inputs["clean"]; input != nil && input.Value != nil {
		clean := input.Value.Value
		if input.Value.ContainsExpression() {
			literal, known := workflowExpressionLiteral(input.Value)
			value, scalar := workflowScalarString(literal)
			if !known || !scalar {
				return
			}
			clean = value
		}
		if clean != "true" {
			return
		}
	}
	checkout := ""
	if input := action.Inputs["path"]; input != nil && input.Value != nil {
		value := workingDirectoryValue(input.Value)
		if value.kind != directoryKnown || !localRunnerPath(value.path) {
			return
		}
		checkout = path.Clean(value.path)
		if checkout == ".." || strings.HasPrefix(checkout, "../") {
			return
		}
	}
	rule.paths.checkout = checkout
	rule.pristine = true
	rule.repositoryUnknown = false
	clear(rule.changed)
}

func (rule *RuleExecutableBit) checkScript(run *ExecRun) {
	if run.Run == nil {
		return
	}
	shell := resolveRunShell(run, rule.jobShell, rule.workflowShell, shellValue{})
	shellName := strings.ToLower(shell.name)
	if shellName != "bash" && shellName != "sh" {
		rule.pristine = false
		return
	}
	// Expressions can inject shell syntax, including additional commands.
	if run.Run.ContainsExpression() {
		rule.pristine = false
		return
	}
	variant := syntax.LangBash
	if shellName == "sh" {
		variant = syntax.LangPOSIX
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(run.Run.Value), "")
	if err != nil {
		rule.pristine = false
		return
	}
	directory := rule.paths.effectiveRunDirectory(run, rule.jobDir, rule.workflowDir)
	for _, statement := range file.Stmts {
		rule.statement(statement, run, &directory)
	}
}

func (rule *RuleExecutableBit) statement(statement *syntax.Stmt, run *ExecRun, directory *runDirectory) {
	if !rule.pristine {
		return
	}
	if statement.Background || statement.Negated || len(statement.Redirs) != 0 {
		rule.pristine = false
		return
	}
	switch command := statement.Cmd.(type) {
	case *syntax.CallExpr:
		rule.call(command, run, directory)
	case *syntax.BinaryCmd:
		if command.Op != syntax.AndStmt {
			rule.pristine = false
			return
		}
		rule.statement(command.X, run, directory)
		rule.statement(command.Y, run, directory)
	default:
		// Functions, control flow and subshells need a separate execution model.
		rule.pristine = false
	}
}

func (rule *RuleExecutableBit) call(command *syntax.CallExpr, run *ExecRun, directory *runDirectory) {
	if len(command.Args) == 0 {
		rule.pristine = false
		return
	}
	for _, assignment := range command.Assigns {
		if assignment.Append || assignment.Naked || assignment.Index != nil || assignment.Array != nil {
			rule.pristine = false
			return
		}
		if assignment.Name != nil {
			switch assignment.Name.Value {
			case "UID", "EUID", "PPID", "BASHOPTS", "SHELLOPTS", "BASH_VERSINFO":
				// Bash may also provide sh; readonly assignments can abort before invocation.
				rule.pristine = false
				return
			}
		}
		if assignment.Value != nil {
			if !simpleShellArgument(assignment.Value.Parts) {
				rule.pristine = false
				return
			}
		}
	}
	name, literal := literalShellWord(command.Args[0].Parts)
	if !literal {
		rule.pristine = false
		return
	}
	if strings.Contains(name, "/") {
		for _, word := range command.Args[1:] {
			if !simpleShellArgument(word.Parts) {
				rule.pristine = false
				return
			}
		}
		rule.checkInvocation(run, command.Args[0], *directory, name)
		rule.pristine = false
		return
	}
	if len(command.Assigns) != 0 {
		// Prefix assignments can alter builtin behavior and subsequent shell state.
		rule.pristine = false
		return
	}
	args := make([]string, len(command.Args))
	for i, word := range command.Args {
		value, ok := literalShellWord(word.Parts)
		if !ok {
			rule.pristine = false
			return
		}
		args[i] = value
	}
	switch args[0] {
	case "cd":
		if len(args) == 2 && directory.kind == directoryKnown && localRunnerPath(args[1]) && !strings.HasPrefix(args[1], "-") {
			candidate := joinRunnerPath(directory.path, args[1])
			snapshot := rule.index()
			if snapshot.err == nil && snapshot.ordinaryTraversal(candidate+"/", rule.paths.checkout) {
				directory.path = candidate
				return
			}
		}
		*directory = runDirectory{kind: directoryUnknown}
	case "chmod":
		// Any literal chmod makes the Git-index mode obsolete for its operands.
		if len(args) < 3 || !knownChmodMode.MatchString(args[1]) || strings.HasPrefix(args[1], "-") {
			rule.pristine = false
			return
		}
		options, hasOperand := true, false
		for _, operand := range args[2:] {
			if options && operand == "--" {
				options = false
				continue
			}
			if options && strings.HasPrefix(operand, "-") {
				rule.pristine = false
				return
			}
			name, ok := rule.scriptPath(*directory, operand)
			if !ok || rule.index().modes[name] == "120000" || rule.index().directoryExists(name) {
				rule.pristine = false
				return
			}
			rule.changed[name] = true
			hasOperand = true
		}
		if !hasOperand {
			rule.pristine = false
		}
	case "echo", ":", "true":
		// Literal arguments and no redirects/substitutions cannot change files.
	default:
		// Includes interpreter/source calls: no executable-bit requirement on the
		// argument, but their execution may change subsequent filesystem state.
		rule.pristine = false
	}
}

// Accept simple parameter reads; substitutions and arithmetic may change files before execution.
func simpleShellArgument(parts []syntax.WordPart) bool {
	for _, part := range parts {
		switch value := part.(type) {
		case *syntax.Lit, *syntax.SglQuoted:
		case *syntax.DblQuoted:
			if value.Dollar || !simpleShellArgument(value.Parts) {
				return false
			}
		case *syntax.ParamExp:
			if value.Param == nil || value.Excl || value.Index != nil || value.Slice != nil || value.Repl != nil || value.Exp != nil {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func localRunnerPath(value string) bool {
	return value != "" && !strings.HasPrefix(value, "/") && !strings.ContainsAny(value, "\\:\x00")
}

func (rule *RuleExecutableBit) scriptPath(directory runDirectory, script string) (string, bool) {
	if directory.kind != directoryKnown || !localRunnerPath(script) || directory.path != "" && !localRunnerPath(directory.path) {
		return "", false
	}
	runnerPath := joinRunnerPath(directory.path, script)
	file, ok := rule.paths.local(runnerPath)
	if !ok {
		return "", false
	}
	snapshot := rule.index()
	if snapshot.err != nil || !snapshot.ordinaryTraversal(runnerPath, rule.paths.checkout) {
		return "", false
	}
	relative, err := filepath.Rel(rule.paths.workspace, file)
	if err != nil || !filepath.IsLocal(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func (rule *RuleExecutableBit) checkInvocation(run *ExecRun, word *syntax.Word, directory runDirectory, script string) {
	if rule.skipFindings {
		return
	}
	name, ok := rule.scriptPath(directory, script)
	if !ok || rule.changed[name] {
		return
	}
	snapshot := rule.index()
	if snapshot.modes[name] != "100644" {
		return
	}
	pos := run.RunPos
	// The shell parser counts bytes; scriptSource uses Unicode columns.
	line, column := int(word.Pos().Line()), int(word.Pos().Col())
	lines := strings.Split(run.Run.Value, "\n")
	if line <= len(lines) && column <= len(lines[line-1])+1 {
		column = utf8.RuneCountInString(lines[line-1][:column-1]) + 1
	}
	if mapped, ok := run.source.pos(line, column); ok {
		pos = mapped
	}
	rule.Errorf(pos, "script %q is executed directly but its Git index mode is 100644 (not executable); commit an executable bit with git update-index --chmod=+x, or invoke its interpreter explicitly", name)
}

func literalShellWord(parts []syntax.WordPart) (string, bool) {
	var out strings.Builder
	for _, part := range parts {
		switch value := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(value.Value, "\\*?[~{}") {
				return "", false
			}
			out.WriteString(value.Value)
		case *syntax.SglQuoted:
			if value.Dollar {
				return "", false
			}
			out.WriteString(value.Value)
		case *syntax.DblQuoted:
			text, ok := literalShellWord(value.Parts)
			if !ok || value.Dollar {
				return "", false
			}
			out.WriteString(text)
		default:
			return "", false
		}
	}
	return out.String(), true
}

func (rule *RuleExecutableBit) index() *gitModeSnapshot {
	snapshot := rule.context.gitModes.load(rule.context.process.ctx, rule.paths.workspace)
	if snapshot.err != nil {
		rule.Debug("Git index unavailable: %v", snapshot.err)
	} else if rule.context.inputs != nil {
		rule.context.inputs.add(snapshot.index)
	}
	return snapshot
}

func joinRunnerPath(base, relative string) string {
	if base == "" {
		return relative
	}
	// Preserve components until symlink traversal has been checked in the index.
	return base + "/" + relative
}

func (snapshot *gitModeSnapshot) ordinaryTraversal(runnerPath, checkout string) bool {
	parts := strings.Split(runnerPath, "/")
	location := ""
	checkout = path.Clean(checkout)
	if checkout == "." {
		checkout = ""
	}
	for i, part := range parts {
		location = path.Join(location, part)
		if location == ".." || strings.HasPrefix(location, "../") {
			return false
		}
		name := location
		if checkout != "" {
			var within bool
			name, within = strings.CutPrefix(location, checkout+"/")
			if !within {
				if location == "." || location == checkout || strings.HasPrefix(checkout, location+"/") {
					continue
				}
				return false
			}
		}
		if i < len(parts)-1 && !snapshot.directoryExists(name) {
			return false
		}
	}
	return true
}

func (snapshot *gitModeSnapshot) directoryExists(name string) bool {
	if name == "" || name == "." {
		return true
	}
	if snapshot.modes[name] != "" {
		return false
	}
	for file := range snapshot.modes {
		if strings.HasPrefix(file, name+"/") {
			return true
		}
	}
	return false
}

// Model common octal and symbolic modes; other syntax leaves execution unknown.
var knownChmodMode = regexp.MustCompile(`^([0-7]{1,4}|[ugoa]*([+=-]([rwxXst]*|[ugo]))+(,[ugoa]*([+=-]([rwxXst]*|[ugo]))+)*)$`)

func shellEnvironmentUnknown(env *Env) bool {
	if env == nil {
		return false
	}
	if env.Expression != nil {
		return true
	}
	for _, variable := range env.Vars {
		if strings.HasPrefix(variable.Name.Value, "BASH_FUNC_") && strings.HasSuffix(variable.Name.Value, "%%") {
			return true
		}
		switch variable.Name.Value {
		case "PATH":
			return true
		case "BASH_ENV", "ENV", "CDPATH", "SHELLOPTS", "BASHOPTS":
			if variable.Value == nil || variable.Value.Value != "" {
				return true
			}
		}
	}
	return false
}
