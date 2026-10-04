package actionlint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Environment files persist across steps, including across composite returns.
// Prove only simple literal shell commands cannot write them; do not execute code.
func (c *LocalActionsCache) observeExecution(step *Step) {
	if enabled, known := invocationCondition(step.If); known && !enabled {
		return
	}
	run, ok := step.Exec.(*ExecRun)
	if !ok {
		return
	}
	if c.shellEnvUnknown || c.shellEnvironmentUnknown(step.Env) || !c.runPreservesEnvironment(run) {
		c.persistentEnvUnknown = true
	}
}

func (c *LocalActionsCache) shellEnvironmentUnknown(env *Env) bool {
	return shellEnvironmentUnknownForPlatform(env, c.platform) || shellPathUnknownForPlatform(env, c.platform)
}

func (c *LocalActionsCache) runPreservesEnvironment(run *ExecRun) bool {
	if run.Run == nil || run.Run.ContainsExpression() {
		return false
	}
	shell := resolveRunShell(run, shellValueFromString(&String{Value: c.checkoutShell.name}), shellValue{}, shellValue{kind: shellValueUnknown})
	variant := syntax.LangBash
	switch strings.ToLower(shell.name) {
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
			if _, literal := literalShellWord(word.Parts); !literal {
				return false
			}
		}
		name, _ := literalShellWord(command.Args[0].Parts)
		switch name {
		case "echo", ":", "true", "cd", "chmod":
			return true
		}
	}
	return false
}

func (c *LocalActionsCache) observeOpaqueAction(step *Step, action *ExecAction) {
	if enabled, known := invocationCondition(step.If); known && !enabled {
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
