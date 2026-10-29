package execution

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/mapping"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"

	"mvdan.cc/sh/v3/syntax"
)

// ExecutableChecker checks direct script calls against the Git index, independently
// of host filesystem permissions and of ShellCheck availability.
type ExecutableChecker struct {
	errs []*findingmodel.Finding
	executableState
	context                             ExecutableContext
	workflowDir, jobDir                 Directory
	workflowShell, jobShell             value.Value
	runnerShell                         value.Value
	paths                               Paths
	unix                                bool
	workflowEnv, jobEnv                 bool
	workflowGitEnv                      mapping.Mapping
	jobGitEnv                           bool
	workflowPathUnknown, jobPathUnknown bool
	pathUnknown                         bool
	skipFindings                        bool
	caseInsensitive                     bool
	shIsDash                            bool
	callerRepositoryUnknown             bool
}

// ExecutableContext supplies repository observations and paths to script analysis.
// Index loads a shared immutable snapshot; all shell interpretation stays local.
type ExecutableContext struct {
	Runners                 *runnerlang.Knowledge
	Context                 context.Context
	ProjectRoot, WorkingDir string
	Reader                  *fsread.Reader
	Index                   func(context.Context, string) *IndexSnapshot
	Log                     func(string, ...any)
}

// NewExecutableChecker creates isolated state for one workflow's script execution.
func NewExecutableChecker(context ExecutableContext) *ExecutableChecker {
	return &ExecutableChecker{context: context}
}

// Findings returns direct-execution diagnostics accumulated while checking steps.
func (rule *ExecutableChecker) Findings() []*findingmodel.Finding { return rule.errs }

// Paths provides the resolved checkout scope for composite invocation adaptation.
func (rule *ExecutableChecker) Paths() *Paths { return &rule.paths }

func (rule *ExecutableChecker) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	rule.errs = append(rule.errs, findingmodel.Errorf(pos, "executable-bit", format, args...))
}

func (rule *ExecutableChecker) debug(format string, args ...any) {
	if rule.context.Log != nil {
		rule.context.Log(format, args...)
	}
}

func (rule *ExecutableChecker) VisitWorkflowPre(declaration workflowdecl.WorkflowView) error {
	if !declaration.Present() {
		return errors.New("executable-bit workflow declaration is unavailable")
	}
	defaults := declaration.Defaults().Run().Semantic()
	env := declaration.Env().Mapping()
	rule.workflowDir = DefaultDirectory(defaults)
	rule.workflowShell = DefaultShell(defaults)
	rule.workflowEnv = ShellEnvironmentUnknown(env)
	rule.workflowGitEnv = env
	rule.workflowPathUnknown = ShellPathUnknown(env)
	rule.callerRepositoryUnknown = false
	for event := range declaration.On() {
		if _, call := event.WorkflowCallEvent(); call {
			rule.callerRepositoryUnknown = true
			break
		}
	}
	return nil
}

func (rule *ExecutableChecker) VisitJobPre(declaration workflowdecl.JobView) error {
	if !declaration.Present() {
		return errors.New("executable-bit job declaration is unavailable")
	}
	defaults := declaration.Defaults().Run().Semantic()
	jobEnv := declaration.Env().Mapping()
	rule.jobDir, rule.jobShell = DefaultDirectory(defaults), DefaultShell(defaults)
	rule.jobEnv = rule.workflowEnv || ShellEnvironmentUnknown(jobEnv)
	knowledge := rule.context.Runners.Job(declaration)
	rule.runnerShell = value.DynamicValue("", value.KindUnknown, value.Origin{})
	if knowledge.Shell != "" {
		rule.runnerShell = value.YAMLScalar(knowledge.Shell, "!!str", value.Origin{})
	}
	platform := knowledge.Platform
	rule.jobGitEnv = CheckoutEnvironmentUnknown(rule.workflowGitEnv, platform) || CheckoutEnvironmentUnknown(jobEnv, platform)
	rule.jobPathUnknown = rule.workflowPathUnknown || ShellPathUnknown(jobEnv)
	rule.unix = platform == runnerlang.MacOrLinux && knowledge.PathsKnown
	rule.caseInsensitive = knowledge.CaseInsensitive
	rule.shIsDash = knowledge.ShIsDash
	// Container mounts can replace the checked-out tree. Treat them as unknown.
	if declaration.Container().Present() || ServicesMayChangeWorkspace(declaration.Services()) {
		rule.unix = false
	}
	hasNeeds := false
	for range declaration.Needs() {
		hasNeeds = true
		break
	}
	if enabled, known := JobCondition(declaration.If().Semantic(), hasNeeds); known && !enabled {
		rule.unix = false
	}
	rule.executableState = newExecutableState(knowledge.Ephemeral, rule.callerRepositoryUnknown)
	rule.paths = Paths{Workspace: rule.context.ProjectRoot, Analysis: rule.context.WorkingDir, Platform: platform, Reader: rule.context.Reader}
	return nil
}

func (rule *ExecutableChecker) VisitStep(declaration workflowdecl.StepView) error {
	if !declaration.Present() {
		return errors.New("executable-bit step declaration is unavailable")
	}
	if !rule.unix || !rule.sequential {
		return nil
	}
	enabled, conditionKnown := Condition(declaration.Condition())
	if conditionKnown && !enabled {
		return nil
	}
	if CanRunAfterFailure(declaration.Condition()) {
		rule.afterPossibleFailure()
	}
	if BoolMayBeTrue(declaration.Background().Semantic()) {
		rule.afterConcurrentExecution()
		return nil
	}
	kind, _ := declaration.Exec().Kind()
	switch kind {
	case workflowlang.ExecKindParallel:
		rule.afterConcurrentExecution()
	case workflowlang.ExecKindAction:
		action, ok := declaration.Action()
		if !ok {
			return nil
		}
		if rule.jobGitEnv || CheckoutEnvironmentUnknown(declaration.Env().Mapping(), rule.paths.Platform) {
			rule.repositoryUnknown = true
			rule.actionPristine = false
		}
		if !action.Uses().Present() {
			rule.actionPristine = false
		} else if name, _, versioned := strings.Cut(action.Uses().Value(), "@"); !versioned || !strings.EqualFold(name, "actions/checkout") {
			rule.actionPristine = false
		}
		rule.checkout(action, !conditionKnown || BoolMayBeTrue(declaration.ContinueOnError().Semantic()))
	case workflowlang.ExecKindRun:
		runView, ok := declaration.Run()
		if !ok {
			return nil
		}
		workspacePristine := rule.pristine
		independent := rule.paths.EffectiveDirectory(runView.WorkingDirectory().Semantic(), rule.jobDir, rule.workflowDir).Kind == DirectoryActionKnown
		if independent {
			rule.pristine = rule.actionPristine
		}
		rule.pathUnknown = rule.jobPathUnknown || ShellPathUnknown(declaration.Env().Mapping())
		if rule.jobEnv || ShellEnvironmentUnknown(declaration.Env().Mapping()) {
			rule.pristine = false
		}
		if rule.pristine {
			rule.checkScript(runView)
		}
		if !conditionKnown {
			// Its invocation sees the current state, but its effects may be skipped.
			rule.pristine = false
		}
		if !rule.pristine {
			rule.actionPristine = false
		}
		if independent {
			rule.pristine = workspacePristine && rule.pristine
		}
		if !rule.pristine {
			rule.repositoryUnknown = true
		}
	case workflowlang.ExecKindWait, workflowlang.ExecKindCancel:
	}
	return nil
}

func (rule *ExecutableChecker) checkout(action workflowdecl.ExecActionView, mayNotComplete bool) {
	rule.pristine = false
	if rule.repositoryUnknown {
		return
	}
	// Opaque execution can change Git settings that preserve working-tree modes.
	rule.repositoryUnknown = true
	if !action.Uses().Present() || mayNotComplete {
		return
	}
	name, ref, versioned := strings.Cut(action.Uses().Value(), "@")
	if !versioned || ref == "" || !strings.EqualFold(name, "actions/checkout") {
		return
	}
	for _, input := range []string{"repository", "ref", "sparse-checkout", "github-server-url"} {
		if inputValue, known := checkoutInput(action.ResolvedInputs(), input); !known || inputValue != "" {
			return
		}
	}
	if clean, known := checkoutInput(action.ResolvedInputs(), "clean"); !known || clean != "" && !strings.EqualFold(clean, "true") {
		return
	}
	checkout, known := checkoutInput(action.ResolvedInputs(), "path")
	checkout, representable := RunnerRelativePath(checkout, rule.paths.Platform)
	if !known || !representable {
		return
	}
	if checkout != "" {
		checkout = path.Clean(checkout)
	}
	if checkout == ".." || strings.HasPrefix(checkout, "../") {
		return
	}
	rule.paths.Checkout = checkout
	rule.afterKnownCheckout(checkout, rule.caseInsensitive)
}

func checkoutInput(inputs mapping.Mapping, name string) (string, bool) {
	item := inputs.Lookup(name)
	if item.State() == value.Missing {
		return "", true
	}
	text, known := item.ScalarString()
	return strings.TrimSpace(text), known
}

func (rule *ExecutableChecker) checkScript(run workflowdecl.ExecRunView) {
	semanticRun := run.Run().Semantic()
	runText, known := semanticRun.Text()
	if !known {
		if semanticRun.State() != value.Missing {
			rule.pristine = false
		}
		return
	}
	shell := ResolveShell(run.Shell().Semantic(), rule.jobShell, rule.workflowShell, rule.runnerShell)
	shellName := strings.ToLower(shell.Name)
	if shellName != "bash" && shellName != "sh" {
		rule.pristine = false
		return
	}
	// Expressions can inject shell syntax, including additional commands.
	if run.Run().Semantic().State() == value.Dynamic || run.Run().Semantic().State() == value.Invalid {
		rule.pristine = false
		return
	}
	variant := syntax.LangBash
	if shellName == "sh" {
		variant = syntax.LangPOSIX
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(runText), "")
	if err != nil {
		rule.pristine = false
		return
	}
	directory := rule.paths.EffectiveDirectory(run.WorkingDirectory().Semantic(), rule.jobDir, rule.workflowDir)
	for _, statement := range file.Stmts {
		rule.statement(statement, run, runText, &directory)
	}
}

func (rule *ExecutableChecker) statement(statement *syntax.Stmt, run workflowdecl.ExecRunView, runText string, directory *Directory) {
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
		rule.call(command, run, runText, directory)
	case *syntax.BinaryCmd:
		if command.Op == syntax.Pipe || command.Op == syntax.PipeAll {
			if invocation, known := pipelineInvocation(statement); known && invocation != nil {
				rule.statement(invocation, run, runText, directory)
			}
			rule.pristine = false
			return
		}
		if command.Op == syntax.OrStmt {
			rule.statement(command.X, run, runText, directory)
			rule.pristine = false
			return
		}
		if command.Op != syntax.AndStmt {
			rule.pristine = false
			return
		}
		rule.statement(command.X, run, runText, directory)
		rule.statement(command.Y, run, runText, directory)
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
		name, known := LiteralShellWord(command.Args[0].Parts)
		if known && len(command.Assigns) == 0 && (name == "true" || name == ":" || name == "echo") {
			for _, word := range command.Args[1:] {
				if _, literal := LiteralShellWord(word.Parts); !literal {
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

func (rule *ExecutableChecker) redirectsKnown(redirects []*syntax.Redirect, directory Directory) bool {
	for _, redirect := range redirects {
		if redirect.Word == nil || redirect.Hdoc != nil || redirect.N != nil && !standardShellDescriptor(redirect.N.Value) {
			return false
		}
		target, literal := LiteralShellWord(redirect.Word.Parts)
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
		case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.AppClob, syntax.RdrAllClob, syntax.AppAllClob:
			return false
		default:
			// Operators added by later mvdan.cc/sh releases are not modelled.
			return false
		}
		if target == "/dev/null" {
			continue
		}
		location, known := rule.scriptPath(directory, target)
		if !known || rule.modeChanges(directory)[location.runner] {
			return false
		}
		name := location.repository
		// Checkout creates Git metadata outside the tracked index tree.
		if directory.Kind == DirectoryKnown && (name == ".git" || rule.caseInsensitive && strings.EqualFold(name, ".git")) {
			return false
		}
		snapshot := rule.index()
		switch snapshot.Modes[name] {
		case "100644", "100755":
		case "":
			if redirect.Op == syntax.RdrIn || snapshot.DirectoryExists(name) {
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

func (rule *ExecutableChecker) call(command *syntax.CallExpr, run workflowdecl.ExecRunView, runText string, directory *Directory) {
	if len(command.Args) == 0 {
		rule.pristine = false
		return
	}
	shell := ResolveShell(run.Shell().Semantic(), rule.jobShell, rule.workflowShell, rule.runnerShell)
	bashReadonly := !rule.shIsDash || !strings.EqualFold(shell.Name, "sh")
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
	name, literal := LiteralShellWord(command.Args[0].Parts)
	if !literal {
		rule.pristine = false
		return
	}
	arguments := command.Args
	if name == "exec" || name == "command" {
		arguments = arguments[1:]
		if len(arguments) > 0 {
			if option, literal := LiteralShellWord(arguments[0].Parts); literal && option == "--" && (name != "exec" || strings.EqualFold(shell.Name, "bash")) {
				arguments = arguments[1:]
			}
		}
		if len(arguments) == 0 {
			rule.pristine = false
			return
		}
		name, literal = LiteralShellWord(arguments[0].Parts)
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
		rule.checkInvocation(run, runText, arguments[0], *directory, name)
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
		value, ok := LiteralShellWord(word.Parts)
		if !ok {
			rule.pristine = false
			return
		}
		args[i] = value
	}
	switch args[0] {
	case "cd":
		if len(args) == 2 && (directory.Kind == DirectoryKnown || directory.Kind == DirectoryActionKnown) && args[1] != "" && !strings.HasPrefix(args[1], "-") {
			destination, representable := RunnerRelativePath(args[1], rule.paths.Platform)
			candidate := JoinPath(directory.Path, destination)
			if _, known := rule.checkedRunnerPathFor(candidate+"/", rule.paths.DirectoryOrigin(*directory)); known && representable {
				directory.Path = candidate
				return
			}
		}
		*directory = Directory{Kind: DirectoryUnknown}
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
			location, ok := rule.scriptPath(*directory, operand)
			mode := rule.index().Modes[location.repository]
			if !ok || mode != "100644" && mode != "100755" {
				rule.pristine = false
				return
			}
			rule.modeChanges(*directory)[location.runner] = true
			if directory.Kind == DirectoryActionKnown {
				// Workspace checkout cannot reset modes in the independent action copy.
				rule.actionPristine = false
			}
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

// Accept simple parameter reads; substitutions and arithmetic may change files before
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

// Repository paths identify indexed modes; runner paths distinguish copies.
type executableLocation struct {
	repository, runner string
}

func (rule *ExecutableChecker) scriptPath(directory Directory, script string) (executableLocation, bool) {
	if directory.Kind != DirectoryKnown && directory.Kind != DirectoryActionKnown || script == "" {
		return executableLocation{}, false
	}
	script, scriptKnown := RunnerRelativePath(script, rule.paths.Platform)
	directoryPath, directoryKnown := RunnerRelativePath(directory.Path, rule.paths.Platform)
	directory.Path = directoryPath
	if !scriptKnown || !directoryKnown {
		return executableLocation{}, false
	}
	runnerPath := JoinPath(directory.Path, script)
	paths := rule.paths.DirectoryOrigin(directory)
	runnerPath, ok := rule.checkedRunnerPathFor(runnerPath, paths)
	if !ok {
		return executableLocation{}, false
	}
	file, ok := paths.Local(runnerPath)
	if !ok {
		return executableLocation{}, false
	}
	relative, err := filepath.Rel(rule.paths.Workspace, file)
	if err != nil || !filepath.IsLocal(relative) {
		return executableLocation{}, false
	}
	runnerPath = path.Clean(runnerPath)
	if rule.caseInsensitive {
		runnerPath = strings.ToLower(runnerPath)
	}
	return executableLocation{repository: filepath.ToSlash(relative), runner: runnerPath}, true
}

func (rule *ExecutableChecker) checkInvocation(run workflowdecl.ExecRunView, runText string, word *syntax.Word, directory Directory, script string) {
	if rule.skipFindings {
		return
	}
	location, ok := rule.scriptPath(directory, script)
	if !ok || rule.modeChanges(directory)[location.runner] {
		return
	}
	name := location.repository
	snapshot := rule.index()
	if snapshot.Modes[name] != "100644" {
		return
	}
	pos, _ := run.RunPos()
	// The shell parser counts bytes; scriptSource uses Unicode columns.
	line, column := int(word.Pos().Line()), int(word.Pos().Col())
	lines := strings.Split(runText, "\n")
	if line <= len(lines) && column <= len(lines[line-1])+1 {
		column = utf8.RuneCountInString(lines[line-1][:column-1]) + 1
	}
	if mapped, ok := run.Source().Position(line, column); ok {
		pos = mapped
	}
	rule.Errorf(&pos, "script %q is executed directly but its Git index mode is 100644 (not executable); commit an executable bit with git update-index --chmod=+x, or invoke its interpreter explicitly", name)
}

func (rule *ExecutableChecker) modeChanges(directory Directory) map[string]bool {
	if directory.Kind == DirectoryActionKnown {
		return rule.actionChanged
	}
	return rule.changed
}

func LiteralShellWord(parts []syntax.WordPart) (string, bool) {
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

func (rule *ExecutableChecker) index() *IndexSnapshot {
	snapshot := rule.context.Index(rule.context.Context, rule.paths.Workspace)
	if snapshot.Err != nil {
		rule.debug("Git index unavailable: %v", snapshot.Err)
	} else {
		rule.paths.Reader.Record(snapshot.Path)
	}
	return snapshot
}

// Model common octal and symbolic modes; other syntax leaves execution unknown.
var knownChmodMode = regexp.MustCompile(`^([0-7]{1,4}|[ugoa]*([+=-]([rwxXst]*|[ugo]))+(,[ugoa]*([+=-]([rwxXst]*|[ugo]))+)*)$`)
