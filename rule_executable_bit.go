package actionlint

import (
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

// RuleExecutableBit checks direct script calls against the Git index, independently
// of host filesystem permissions and of ShellCheck availability.
type RuleExecutableBit struct {
	RuleBase
	context                             ruleContext
	workflowDir, jobDir                 runDirectory
	workflowShell, jobShell             shellValue
	paths                               runPaths
	unix, sequential, pristine          bool
	changed                             map[string]bool
	workflowEnv, jobEnv                 bool
	workflowGitEnv, jobGitEnv           bool
	workflowPathUnknown, jobPathUnknown bool
	pathUnknown                         bool
	caseInsensitive                     bool
	shIsDash                            bool
	repositoryUnknown                   bool
	callerRepositoryUnknown             bool
}

func newRuleExecutableBit(context ruleContext) *RuleExecutableBit {
	return &RuleExecutableBit{RuleBase: builtinRuleBase("executable-bit"), context: context}
}

func (rule *RuleExecutableBit) VisitWorkflowPre(workflow *Workflow) error {
	rule.workflowDir = defaultsWorkingDirectory(workflow.Defaults)
	rule.workflowShell = defaultsShellValue(workflow.Defaults)
	rule.workflowEnv = shellEnvironmentUnknown(workflow.Env)
	rule.workflowGitEnv = checkoutEnvironmentUnknown(workflow.Env)
	rule.workflowPathUnknown = shellPathUnknown(workflow.Env)
	_, rule.callerRepositoryUnknown = workflow.FindWorkflowCallEvent()
	return nil
}

func (rule *RuleExecutableBit) VisitJobPre(job *Job) error {
	rule.jobDir, rule.jobShell = defaultsWorkingDirectory(job.Defaults), defaultsShellValue(job.Defaults)
	rule.jobEnv = rule.workflowEnv || shellEnvironmentUnknown(job.Env)
	rule.jobGitEnv = rule.workflowGitEnv || checkoutEnvironmentUnknown(job.Env)
	rule.jobPathUnknown = rule.workflowPathUnknown || shellPathUnknown(job.Env)
	rule.unix = runnerPlatform(job.RunsOn) == platformKindMacOrLinux
	rule.caseInsensitive = macOSRunner(job.RunsOn)
	rule.shIsDash = ubuntuRunner(job.RunsOn)
	// Container mounts can replace the checked-out tree. Treat them as unknown.
	if job.Container != nil || servicesMayChangeWorkspace(job.Services) {
		rule.unix = false
	}
	if enabled, known := invocationCondition(job.If); known && !enabled {
		rule.unix = false
	}
	rule.sequential, rule.pristine = true, false
	rule.repositoryUnknown = rule.callerRepositoryUnknown || !knownHostedRunner(job.RunsOn)
	rule.changed = make(map[string]bool)
	rule.paths = runPaths{workspace: rule.context.projectRoot, analysis: rule.context.workingDir, platform: runnerPlatform(job.RunsOn)}
	return nil
}

func (rule *RuleExecutableBit) VisitStep(step *Step) error {
	if !rule.unix || !rule.sequential {
		return nil
	}
	enabled, conditionKnown := invocationCondition(step.If)
	if conditionKnown && !enabled {
		return nil
	}
	if stepCanRunAfterFailure(step.If) {
		rule.pristine, rule.repositoryUnknown = false, true
	}
	if boolMayBeTrue(step.Background) {
		rule.sequential, rule.pristine = false, false
		return nil
	}
	switch command := step.Exec.(type) {
	case *ExecParallel:
		rule.sequential, rule.pristine = false, false
	case *ExecAction:
		if rule.jobGitEnv || checkoutEnvironmentUnknown(step.Env) {
			rule.repositoryUnknown = true
		}
		rule.checkout(command, !conditionKnown || boolMayBeTrue(step.ContinueOnError))
	case *ExecRun:
		rule.pathUnknown = rule.jobPathUnknown || shellPathUnknown(step.Env)
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
	if !known {
		// Explicit success() has the same gate as an ordinary step.
		if call, ok := parseAssignedExpression(expression.Value).(*FuncCallNode); ok && strings.EqualFold(call.Callee, "success") && len(call.Args) == 0 {
			return true, true
		}
		return false, false
	}
	switch value := value.(type) {
	case nil:
		return false, true
	case bool:
		return value, true
	case float64:
		return value != 0, true
	case string:
		return value != "", true
	default:
		// Known JSON arrays and objects are truthy, including empty ones.
		return true, true
	}
}

func stepCanRunAfterFailure(condition *String) bool {
	if condition == nil {
		return false
	}
	source := condition.Value
	if !condition.ContainsExpression() {
		source = "${{ " + source + " }}"
	}
	expression := parseAssignedExpression(source)
	if expression == nil {
		return true
	}
	hasStatusFunction := false
	VisitExprNode(expression, func(node, _ ExprNode, entering bool) {
		if call, ok := node.(*FuncCallNode); entering && ok {
			switch strings.ToLower(call.Callee) {
			case "always", "cancelled", "failure", "success":
				hasStatusFunction = true
			}
		}
	})
	// A status function removes the runner's implicit success() condition.
	return hasStatusFunction && !expressionRequiresSuccess(expression)
}

func expressionRequiresSuccess(expression ExprNode) bool {
	switch node := expression.(type) {
	case *FuncCallNode:
		return strings.EqualFold(node.Callee, "success") && len(node.Args) == 0
	case *LogicalOpNode:
		left, right := expressionRequiresSuccess(node.Left), expressionRequiresSuccess(node.Right)
		switch node.Kind {
		case LogicalOpNodeKindAnd:
			return left || right
		case LogicalOpNodeKindOr:
			return left && right
		default:
			return false
		}
	default:
		return false
	}
}

func knownHostedRunner(runner *Runner) bool {
	if runner == nil || runner.Group != nil {
		return false
	}
	for _, expression := range []*String{runner.Expression, runner.LabelsExpr} {
		if expression == nil {
			continue
		}
		value, known := workflowExpressionLiteral(expression)
		if !known {
			return false
		}
		if selection, ok := value.(map[string]any); ok {
			for key := range selection {
				if !strings.EqualFold(key, "labels") {
					return false
				}
			}
		}
	}
	labels := runnerPlatformLabels(runner)
	if len(labels) != 1 {
		return false
	}
	for _, label := range labels {
		if !slices.Contains(allGitHubHostedRunnerLabels, strings.ToLower(label.Value)) {
			return false
		}
	}
	return len(labels) != 0
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
	name, ref, versioned := strings.Cut(action.Uses.Value, "@")
	if !versioned || ref == "" || !strings.EqualFold(name, "actions/checkout") {
		return
	}
	for _, input := range []string{"repository", "ref", "sparse-checkout", "github-server-url"} {
		if value, known := checkoutInput(action, input); !known || value != "" {
			return
		}
	}
	if clean, known := checkoutInput(action, "clean"); !known || clean != "" && !strings.EqualFold(clean, "true") {
		return
	}
	checkout, known := checkoutInput(action, "path")
	checkout, representable := runnerRelativePath(checkout, rule.paths.platform)
	if !known || !representable {
		return
	}
	if checkout != "" {
		checkout = path.Clean(checkout)
	}
	if checkout == ".." || strings.HasPrefix(checkout, "../") {
		return
	}
	rule.paths.checkout = checkout
	rule.pristine = true
	rule.repositoryUnknown = false
	clear(rule.changed)
}

func checkoutInput(action *ExecAction, name string) (string, bool) {
	input := action.Inputs[name]
	if input == nil || input.Value == nil {
		return "", true
	}
	value := input.Value.Value
	if input.Value.ContainsExpression() {
		literal, known := workflowExpressionLiteral(input.Value)
		text, scalar := workflowScalarString(literal)
		if !known || !scalar {
			return "", false
		}
		value = text
	}
	return strings.TrimSpace(value), true
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
	if statement.Background || !rule.redirectsKnown(statement.Redirs, *directory) {
		rule.pristine = false
		return
	}
	if statement.Negated || len(statement.Redirs) != 0 {
		// Negation changes continuation; redirects can overwrite later inputs.
		defer func() { rule.pristine = false }()
	}
	switch command := statement.Cmd.(type) {
	case *syntax.CallExpr:
		rule.call(command, run, directory)
	case *syntax.BinaryCmd:
		if command.Op == syntax.Pipe || command.Op == syntax.PipeAll {
			if invocation, known := pipelineInvocation(statement); known && invocation != nil {
				rule.statement(invocation, run, directory)
			}
			rule.pristine = false
			return
		}
		if command.Op == syntax.OrStmt {
			rule.statement(command.X, run, directory)
			rule.pristine = false
			return
		}
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

// A single invocation can be checked against incoming modes only when every
// concurrent sibling is a literal, nonmutating builtin. Afterwards state is unknown.
func pipelineInvocation(statement *syntax.Stmt) (*syntax.Stmt, bool) {
	if statement.Background || len(statement.Redirs) != 0 {
		return nil, false
	}
	switch command := statement.Cmd.(type) {
	case *syntax.BinaryCmd:
		if command.Op != syntax.Pipe && command.Op != syntax.PipeAll {
			return nil, false
		}
		left, leftKnown := pipelineInvocation(command.X)
		right, rightKnown := pipelineInvocation(command.Y)
		if !leftKnown || !rightKnown || left != nil && right != nil {
			return nil, false
		}
		if left != nil {
			return left, true
		}
		return right, true
	case *syntax.CallExpr:
		if len(command.Args) == 0 {
			return nil, false
		}
		name, known := literalShellWord(command.Args[0].Parts)
		if known && len(command.Assigns) == 0 && (name == "true" || name == ":" || name == "echo") {
			for _, word := range command.Args[1:] {
				if _, literal := literalShellWord(word.Parts); !literal {
					return nil, false
				}
			}
			return nil, true
		}
		return statement, true
	default:
		return nil, false
	}
}

func (rule *RuleExecutableBit) redirectsKnown(redirects []*syntax.Redirect, directory runDirectory) bool {
	for _, redirect := range redirects {
		if redirect.Word == nil || redirect.Hdoc != nil || redirect.N != nil && !standardShellDescriptor(redirect.N.Value) {
			return false
		}
		target, literal := literalShellWord(redirect.Word.Parts)
		if !literal {
			return false
		}
		switch redirect.Op {
		case syntax.DplIn, syntax.DplOut:
			if !standardShellDescriptor(target) {
				return false
			}
			continue
		case syntax.RdrIn, syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll:
		default:
			return false
		}
		if target == "/dev/null" {
			continue
		}
		name, known := rule.scriptPath(directory, target)
		if !known || rule.changed[name] {
			return false
		}
		// Checkout creates Git metadata outside the tracked index tree.
		if directory.kind == directoryKnown && (name == ".git" || rule.caseInsensitive && strings.EqualFold(name, ".git")) {
			return false
		}
		snapshot := rule.index()
		switch snapshot.modes[name] {
		case "100644", "100755":
		case "":
			if redirect.Op == syntax.RdrIn || snapshot.directoryExists(name) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func standardShellDescriptor(value string) bool {
	return value == "0" || value == "1" || value == "2"
}

func (rule *RuleExecutableBit) call(command *syntax.CallExpr, run *ExecRun, directory *runDirectory) {
	if len(command.Args) == 0 {
		rule.pristine = false
		return
	}
	shell := resolveRunShell(run, rule.jobShell, rule.workflowShell, shellValue{})
	bashReadonly := !rule.shIsDash || !strings.EqualFold(shell.name, "sh")
	for _, assignment := range command.Assigns {
		if assignment.Append || assignment.Naked || assignment.Index != nil || assignment.Array != nil {
			rule.pristine = false
			return
		}
		if bashReadonly && assignment.Name != nil {
			switch assignment.Name.Value {
			case "UID", "EUID", "PPID", "BASHOPTS", "SHELLOPTS", "BASH_VERSINFO":
				// Outside known Ubuntu sh, Bash may provide sh and abort on readonly names.
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
	arguments := command.Args
	if name == "exec" || name == "command" {
		arguments = arguments[1:]
		if len(arguments) > 0 {
			if option, literal := literalShellWord(arguments[0].Parts); literal && option == "--" && (name != "exec" || strings.EqualFold(shell.name, "bash")) {
				arguments = arguments[1:]
			}
		}
		if len(arguments) == 0 {
			rule.pristine = false
			return
		}
		name, literal = literalShellWord(arguments[0].Parts)
		// Lookup-only flags and other options do not prove a script invocation.
		if !literal || strings.HasPrefix(name, "-") || !strings.Contains(name, "/") {
			rule.pristine = false
			return
		}
	}
	if strings.Contains(name, "/") {
		for _, word := range arguments[1:] {
			if !simpleShellArgument(word.Parts) {
				rule.pristine = false
				return
			}
		}
		rule.checkInvocation(run, arguments[0], *directory, name)
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
		if len(args) == 2 && directory.kind == directoryKnown && args[1] != "" && !strings.HasPrefix(args[1], "-") {
			destination, representable := runnerRelativePath(args[1], rule.paths.platform)
			candidate := joinRunnerPath(directory.path, destination)
			if _, known := rule.checkedRunnerPath(candidate + "/"); known && representable {
				directory.path = candidate
				return
			}
		}
		*directory = runDirectory{kind: directoryUnknown}
	case "chmod":
		if rule.pathUnknown {
			rule.pristine = false
			return
		}
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
			mode := rule.index().modes[name]
			if !ok || mode != "100644" && mode != "100755" {
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

func (rule *RuleExecutableBit) scriptPath(directory runDirectory, script string) (string, bool) {
	if directory.kind != directoryKnown || script == "" {
		return "", false
	}
	script, scriptKnown := runnerRelativePath(script, rule.paths.platform)
	directoryPath, directoryKnown := runnerRelativePath(directory.path, rule.paths.platform)
	directory.path = directoryPath
	if !scriptKnown || !directoryKnown {
		return "", false
	}
	runnerPath := joinRunnerPath(directory.path, script)
	runnerPath, ok := rule.checkedRunnerPath(runnerPath)
	if !ok {
		return "", false
	}
	file, ok := rule.paths.local(runnerPath)
	if !ok {
		return "", false
	}
	relative, err := filepath.Rel(rule.paths.workspace, file)
	if err != nil || !filepath.IsLocal(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func (rule *RuleExecutableBit) checkInvocation(run *ExecRun, word *syntax.Word, directory runDirectory, script string) {
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
			if value.Dollar {
				return "", false
			}
			for _, part := range value.Parts {
				literal, ok := part.(*syntax.Lit)
				if !ok || strings.ContainsRune(literal.Value, '\\') {
					return "", false
				}
				out.WriteString(literal.Value)
			}
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
	snapshot.prepareDirectories()
	_, exists := snapshot.dirs[name]
	return exists
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
		name, known := environmentLiteral(variable.Name)
		if !known || loaderEnvironmentUnknown(name, variable.Value) {
			return true
		}
		if strings.HasPrefix(name, "BASH_FUNC_") && strings.HasSuffix(name, "%%") {
			return true
		}
		switch name {
		case "BASH_ENV", "ENV", "CDPATH", "SHELLOPTS", "BASHOPTS":
			if value, known := environmentLiteral(variable.Value); !known || value != "" {
				return true
			}
		}
	}
	return false
}

func shellPathUnknown(env *Env) bool {
	if env != nil {
		for _, variable := range env.Vars {
			if name, known := environmentLiteral(variable.Name); !known || name == "PATH" {
				return true
			}
		}
	}
	return false
}
