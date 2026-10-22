package expressions

import (
	"path"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
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
			runnerShell = execution.ShellValue{Kind: execution.ShellUnknown}
			if shell := rule.dependencies.Runners.Job(rule.scriptJob).Shell; shell != "" {
				runnerShell = execution.ShellValueOf(&workflowlang.String{Value: shell})
			}
		}
	}
	return execution.ResolveShell(run, jobShell, workflowShell, runnerShell).Name
}

func scriptShellVariant(shell string) (syntax.LangVariant, bool) {
	words := strings.Fields(shell)
	if len(words) == 0 || strings.Contains(shell, "${{") {
		return syntax.LangBash, false
	}
	if len(words) > 1 && !directShellScript(words[1:]) {
		return syntax.LangBash, false
	}
	switch path.Base(strings.ReplaceAll(words[0], "\\", "/")) {
	case "bash", "bash.exe":
		return syntax.LangBash, true
	case "sh", "sh.exe":
		return syntax.LangPOSIX, true
	default:
		return syntax.LangBash, false
	}
}

func directShellScript(arguments []string) bool {
	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		switch argument {
		case "{0}", "'{0}'", `"{0}"`:
			return true
		case "--":
			return i+1 < len(arguments) && directShellScript(arguments[i+1:i+2])
		case "--noprofile", "--norc", "--posix", "--restricted", "--verbose", "--login", "--noediting":
			continue
		}
		if len(argument) < 2 || argument[0] != '-' && argument[0] != '+' {
			return false
		}
		for j, option := range argument[1:] {
			if option == 'o' || option == 'O' {
				if j != len(argument)-2 || i+1 == len(arguments) || strings.ContainsAny(arguments[i+1], "{}+-") {
					return false
				}
				i++
			} else if !strings.ContainsRune("aefhkmnptuvxBCEHPTilr", option) {
				return false
			}
		}
	}
	return false
}

// Scalar exceptions cover echo/printf data arguments in parsed POSIX shells.
// This proves lexical position, not runtime command identity or argument semantics.
func scalarScriptArguments(source, shell string) map[int]bool {
	variant, ok := scriptShellVariant(shell)
	if !ok {
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
		// Numeric masks preserve IO-number tokens before shell redirections.
		for i := first; i < last; i++ {
			if masked[i] != '\n' && masked[i] != '\r' {
				masked[i] = '0'
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
