package workspace

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/scripts"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"

	"mvdan.cc/sh/v3/syntax"
)

// ObserveExecution tracks environment-file effects across steps and composite returns.
func (c *Resolver) ObserveExecution(step workflows.Step) {
	if enabled, known := conditions.Condition(step.Condition()); known && !enabled {
		return
	}
	run, ok := step.Run()
	if !ok {
		return
	}
	if c.shellEnvUnknown || c.shellEnvironmentUnknown(workflows.Environment(step.Env)) || !c.runPreservesEnvironment(workflows.Scalar(run.Run), workflows.Scalar(run.Shell)) {
		c.persistentEnvUnknown = true
	}
}

func (c *Resolver) shellEnvironmentUnknown(env mappings.Mapping) bool {
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
			if _, literal := scripts.LiteralShellWord(word.Parts); !literal {
				return false
			}
		}
		name, _ := scripts.LiteralShellWord(command.Args[0].Parts)
		switch name {
		case "echo", ":", "true", "cd", "chmod":
			return true
		}
	}
	return false
}

// ObserveOpaqueAction invalidates persistent environment assumptions for opaque actions.
func (c *Resolver) ObserveOpaqueAction(step workflows.Step, action *definition.ExecAction) {
	if enabled, known := conditions.Condition(step.Condition()); known && !enabled {
		return
	}
	if action != nil && action.Uses != nil {
		name, _, versioned := strings.Cut(action.Uses.Value, "@")
		if versioned && strings.EqualFold(name, "actions/checkout") {
			return
		}
	}
	c.persistentEnvUnknown = true
}
