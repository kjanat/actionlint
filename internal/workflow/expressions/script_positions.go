package expressions

import (
	"path"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func (rule *Checker) scriptShell(step workflowdecl.StepView) string {
	run, ok := step.Run()
	if !ok {
		return ""
	}
	var jobShell, workflowShell, runnerShell value.Value
	if !rule.compositeScript {
		if rule.workflow.Present() {
			workflowShell = execution.DefaultShell(rule.workflow.Defaults().Run().Semantic())
			if job := rule.scriptJob; job.Present() {
				jobShell = execution.DefaultShell(job.Defaults().Run().Semantic())
				runnerShell = execution.ContainerShell(job.Container().Semantic())
				if runnerShell.State() == value.Missing {
					runnerShell = value.DynamicValue("", value.KindUnknown, value.Origin{})
					if rule.dependencies.Runners != nil {
						if shell := rule.dependencies.Runners.Job(job).Shell; shell != "" {
							runnerShell = value.YAMLScalar(shell, "!!str", value.Origin{})
						}
					}
				}
			}
		} else {
			runnerShell = value.DynamicValue("", value.KindUnknown, value.Origin{})
		}
	}
	return execution.ResolveShell(run.Shell().Semantic(), jobShell, workflowShell, runnerShell).Name
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

type scriptArgumentProofs struct {
	scalars   map[int]bool
	wordLists map[int]bool
}

// Scalar exceptions cover echo/printf data arguments. Closed literal word lists
// may occupy a whole literal argument of a statically named command. Neither
// proof validates the command's own argument semantics.
func proveScriptArguments(source, shell string) scriptArgumentProofs {
	variant, ok := scriptShellVariant(shell)
	if !ok {
		return scriptArgumentProofs{}
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
			return scriptArgumentProofs{}
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
		return scriptArgumentProofs{}
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(string(masked)), "")
	if err != nil {
		return scriptArgumentProofs{}
	}
	proofs := scriptArgumentProofs{scalars: map[int]bool{}, wordLists: map[int]bool{}}
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		command := call.Args[0].Lit()
		staticCommand := command != "" && !strings.Contains(source[call.Args[0].Pos().Offset():call.Args[0].End().Offset()], "${{")
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
				if command == "echo" || command == "printf" {
					proofs.scalars[expression.first] = true
				}
				if staticCommand && wholeLiteralArgument(argument, expression.first, expression.last) {
					proofs.wordLists[expression.first] = true
				}
			}
		}
		return true
	})
	return proofs
}

func wholeLiteralArgument(argument *syntax.Word, expressionFirst, expressionLast int) bool {
	if len(argument.Parts) != 1 {
		return false
	}
	first, last := int(argument.Pos().Offset()), int(argument.End().Offset())
	switch part := argument.Parts[0].(type) {
	case *syntax.Lit:
	case *syntax.SglQuoted:
		if part.Dollar {
			return false
		}
		first, last = first+1, last-1
	case *syntax.DblQuoted:
		if part.Dollar || !literalScriptWord(part.Parts) {
			return false
		}
		first, last = first+1, last-1
	default:
		return false
	}
	return first == expressionFirst && last == expressionLast
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
