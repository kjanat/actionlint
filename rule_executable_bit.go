package actionlint

import (
	"path"
	"path/filepath"
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
	rule.sequential, rule.pristine = true, false
	rule.changed = make(map[string]bool)
	rule.paths = runPaths{workspace: rule.context.projectRoot, analysis: rule.context.workingDir}
	return nil
}

func (rule *RuleExecutableBit) VisitStep(step *Step) error {
	if !rule.unix || !rule.sequential {
		return nil
	}
	if step.Background != nil && (step.Background.Expression != nil || step.Background.Value) {
		rule.sequential, rule.pristine = false, false
		return nil
	}
	switch command := step.Exec.(type) {
	case *ExecParallel:
		rule.sequential, rule.pristine = false, false
	case *ExecAction:
		rule.checkout(step, command)
	case *ExecRun:
		if rule.jobEnv || shellEnvironmentUnknown(step.Env) {
			rule.pristine = false
		}
		if rule.pristine {
			rule.checkScript(command)
		}
	}
	return nil
}

// A known self checkout establishes which index is represented in the workspace.
// Opaque actions may change permissions or replace files, so invalidate that state.
func (rule *RuleExecutableBit) checkout(step *Step, action *ExecAction) {
	rule.pristine = false
	if action.Uses == nil || step.If != nil || action.InputsExpression != nil {
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
	if input := action.Inputs["clean"]; input != nil && input.Value != nil && input.Value.Value != "true" {
		return
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
	clear(rule.changed)
}

func (rule *RuleExecutableBit) checkScript(run *ExecRun) {
	if run.Run == nil {
		return
	}
	shell := resolveRunShell(run, rule.jobShell, rule.workflowShell, shellValue{})
	if shell.name != "bash" && shell.name != "sh" {
		rule.pristine = false
		return
	}
	// Expressions can inject shell syntax, including additional commands.
	if run.Run.ContainsExpression() {
		rule.pristine = false
		return
	}
	variant := syntax.LangBash
	if shell.name == "sh" {
		variant = syntax.LangPOSIX
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(run.Run.Value), "")
	if err != nil {
		rule.pristine = false
		return
	}
	directory := effectiveRunDirectory(run, rule.jobDir, rule.workflowDir)
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
	if len(command.Assigns) != 0 || len(command.Args) == 0 {
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
			directory.path = joinRunnerPath(directory.path, args[1])
		} else {
			*directory = runDirectory{kind: directoryUnknown}
		}
	case "chmod":
		// Any literal chmod makes the Git-index mode obsolete for its operands.
		if len(args) < 3 || strings.HasPrefix(args[1], "-") {
			rule.pristine = false
			return
		}
		for _, operand := range args[2:] {
			name, ok := rule.scriptPath(*directory, operand)
			if !ok {
				rule.pristine = false
				return
			}
			rule.changed[name] = true
		}
	case "echo", ":", "true":
		// Literal arguments and no redirects/substitutions cannot change files.
	default:
		if strings.Contains(args[0], "/") {
			rule.checkInvocation(run, command.Args[0], *directory, args[0])
		}
		// Includes interpreter/source calls: no executable-bit requirement on the
		// argument, but their execution may change subsequent filesystem state.
		rule.pristine = false
	}
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
				continue
			}
		}
		if i < len(parts)-1 && snapshot.modes[name] != "" {
			return false
		}
	}
	return true
}

func shellEnvironmentUnknown(env *Env) bool {
	if env == nil {
		return false
	}
	if env.Expression != nil {
		return true
	}
	for _, variable := range env.Vars {
		switch variable.Name.Value {
		case "BASH_ENV", "ENV", "CDPATH":
			if variable.Value == nil || variable.Value.Value != "" {
				return true
			}
		}
	}
	return false
}
