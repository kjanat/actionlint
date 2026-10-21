package expressions

import (
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	"actionlint.kjanat.dev/internal/workflow/runner"
)

func (rule *Checker) scriptShell(run *workflowlang.ExecRun) string {
	var jobShell, workflowShell, runnerShell execution.ShellValue
	if rule.workflow != nil {
		workflowShell = execution.DefaultShell(rule.workflow.Defaults)
	}
	if rule.scriptJob != nil {
		jobShell = execution.DefaultShell(rule.scriptJob.Defaults)
		runnerShell = execution.ContainerShell(rule.scriptJob.Container)
		if runnerShell.Kind == execution.ShellUnspecified {
			switch runner.PlatformOf(rule.scriptJob.RunsOn) {
			case runner.Windows:
				runnerShell = execution.ShellValueOf(&workflowlang.String{Value: "pwsh"})
			case runner.Any:
				runnerShell = execution.ShellValue{Kind: execution.ShellUnknown}
			}
		}
	}
	return execution.ResolveShell(run, jobShell, workflowShell, runnerShell).Name
}

// Scalar exceptions cover echo/printf data arguments in parsed POSIX shells.
// This proves lexical position, not runtime command identity or argument semantics.
func scalarScriptArguments(source, shell string) map[int]bool {
	variant := syntax.LangBash
	switch shell {
	case "bash":
	case "sh":
		variant = syntax.LangPOSIX
	default:
		return nil
	}
	masked := []byte(source)
	type span struct{ first, last int }
	var expressions []span
	for offset := 0; offset < len(source); {
		index := strings.Index(source[offset:], "${{")
		if index < 0 {
			break
		}
		first := offset + index
		lexer := exprlang.NewLexer(source[first+3:])
		if _, err := exprlang.NewParser().Parse(lexer); err != nil {
			return nil
		}
		last := first + 3 + lexer.Offset()
		expressions = append(expressions, span{first, last})
		for i := first; i < last; i++ {
			if masked[i] != '\n' && masked[i] != '\r' {
				masked[i] = '_'
			}
		}
		offset = last
	}
	if len(expressions) == 0 {
		return nil
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(string(masked)), "")
	if err != nil {
		return nil
	}
	positions := make(map[int]bool)
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		command := call.Args[0].Lit()
		if command != "echo" && command != "printf" {
			return true
		}
		for _, argument := range call.Args[1:] {
			if !literalScriptWord(argument.Parts) {
				continue
			}
			first, last := int(argument.Pos().Offset()), int(argument.End().Offset())
			start := sort.Search(len(expressions), func(i int) bool { return expressions[i].first >= first })
			for _, expression := range expressions[start:] {
				if expression.last > last {
					break
				}
				positions[expression.first] = true
			}
		}
		return true
	})
	return positions
}

func literalScriptWord(parts []syntax.WordPart) bool {
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
		case *syntax.SglQuoted:
			if part.Dollar {
				return false
			}
		case *syntax.DblQuoted:
			if part.Dollar || !literalScriptWord(part.Parts) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
