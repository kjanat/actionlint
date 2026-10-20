package local

import (
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"

	"mvdan.cc/sh/v3/syntax"
)

// ObserveExecution tracks environment-file effects across steps and composite returns.
// Prove only simple literal shell commands cannot write them; do not execute code.
func (c *Resolver) ObserveExecution(step *workflowlang.Step) {
	if enabled, known := execution.Condition(step.If); known && !enabled {
		return
	}
	run, ok := step.Exec.(*workflowlang.ExecRun)
	if !ok {
		return
	}
	if c.shellEnvUnknown || c.shellEnvironmentUnknown(step.Env) || !c.runPreservesEnvironment(run) {
		c.persistentEnvUnknown = true
	}
}

func (c *Resolver) shellEnvironmentUnknown(env *workflowlang.Env) bool {
	return execution.ShellEnvironmentUnknownForPlatform(env, c.platform) || execution.ShellPathUnknownForPlatform(env, c.platform)
}

func (c *Resolver) runPreservesEnvironment(run *workflowlang.ExecRun) bool {
	if run.Run == nil || run.Run.ContainsExpression() {
		return false
	}
	shell := execution.ResolveShell(run, execution.ShellValueOf(&workflowlang.String{Value: c.checkoutShell.Name}), execution.ShellValue{}, execution.ShellValue{Kind: execution.ShellUnknown})
	variant := syntax.LangBash
	switch strings.ToLower(shell.Name) {
	case "bash":
	case "sh":
		variant = syntax.LangPOSIX
	default:
		return false
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(run.Run.Value), "")
	if err != nil {
		return false
	}
	for _, statement := range file.Stmts {
		if !preservesEnvironment(statement) {
			return false
		}
	}
	return true
}

func preservesEnvironment(statement *syntax.Stmt) bool {
	if len(statement.Redirs) != 0 {
		return false
	}
	switch command := statement.Cmd.(type) {
	case *syntax.BinaryCmd:
		return (command.Op == syntax.AndStmt || command.Op == syntax.OrStmt) && preservesEnvironment(command.X) && preservesEnvironment(command.Y)
	case *syntax.CallExpr:
		if len(command.Assigns) != 0 || len(command.Args) == 0 {
			return false
		}
		for _, word := range command.Args {
			if _, literal := execution.LiteralShellWord(word.Parts); !literal {
				return false
			}
		}
		name, _ := execution.LiteralShellWord(command.Args[0].Parts)
		switch name {
		case "echo", ":", "true", "cd", "chmod":
			return true
		}
	}
	return false
}

// ObserveOpaqueAction invalidates persistent environment assumptions for opaque actions.
func (c *Resolver) ObserveOpaqueAction(step *workflowlang.Step, action *workflowlang.ExecAction) {
	if enabled, known := execution.Condition(step.If); known && !enabled {
		return
	}
	if action.Uses != nil {
		name, _, versioned := strings.Cut(action.Uses.Value, "@")
		if versioned && strings.EqualFold(name, "actions/checkout") {
			return
		}
	}
	c.persistentEnvUnknown = true
}
