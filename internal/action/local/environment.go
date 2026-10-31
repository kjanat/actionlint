package local

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/value"

	"mvdan.cc/sh/v3/syntax"
)

// ObserveExecution tracks environment-file effects across steps and composite returns.
func (c *Resolver) ObserveExecution(step workflowdecl.StepView) {
	if enabled, known := execution.Condition(step.Condition()); known && !enabled {
		return
	}
	run, ok := step.Run()
	if !ok {
		return
	}
	if c.shellEnvUnknown || c.shellEnvironmentUnknown(step.Env().Mapping()) || !c.runPreservesEnvironment(run.Run().Semantic(), run.Shell().Semantic()) {
		c.persistentEnvUnknown = true
	}
}

func (c *Resolver) shellEnvironmentUnknown(env mapping.Mapping) bool {
	return execution.ShellEnvironmentUnknownForPlatform(env, c.platform) || execution.ShellPathUnknownForPlatform(env, c.platform)
}

func (c *Resolver) runPreservesEnvironment(run, runShell value.Value) bool {
	source, known := run.Text()
	if !known {
		return false
	}
	checkoutShell := value.DynamicValue("", value.KindUnknown, value.Origin{})
	if c.checkoutShell.Name != "" {
		checkoutShell = value.YAMLScalar(c.checkoutShell.Name, "!!str", value.Origin{})
	}
	shell := execution.ResolveShell(runShell, value.Value{}, value.Value{}, checkoutShell)
	variant := syntax.LangBash
	switch strings.ToLower(shell.Name) {
	case "bash":
	case "sh":
		variant = syntax.LangPOSIX
	default:
		return false
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(source), "")
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
func (c *Resolver) ObserveOpaqueAction(step workflowdecl.StepView, action workflowdecl.ExecActionView) {
	if enabled, known := execution.Condition(step.Condition()); known && !enabled {
		return
	}
	uses := action.Uses()
	if uses.Present() {
		name, _, versioned := strings.Cut(uses.Value(), "@")
		if versioned && strings.EqualFold(name, "actions/checkout") {
			return
		}
	}
	c.persistentEnvUnknown = true
}
