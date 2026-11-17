package expressions

import (
	"path"
	"sort"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	shellsyntax "mvdan.cc/sh/v3/syntax"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"

	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/scripts"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func (rule *Checker) scriptShell(step workflows.BoundStep) string {
	run, ok := step.Run()
	if !ok {
		return ""
	}
	var jobShell, workflowShell, runnerShell value.Value
	if !rule.compositeScript {
		if rule.workflow.Present() {
			workflowShell = execution.DefaultShell(workflows.DefaultsRun(rule.workflow.Defaults))
			if job := rule.scriptJob; job.Present() {
				jobShell = execution.DefaultShell(workflows.DefaultsRun(job.Defaults))
				knownShell := ""
				if rule.dependencies.Runners != nil {
					knownShell = rule.dependencies.Runners.Job(job).Shell
				}
				runnerShell = scripts.DefaultShell(workflows.Container(job.Container), knownShell)
			}
		} else {
			runnerShell = value.DynamicValue("", value.KindUnknown, value.Origin{})
		}
	}
	return execution.ResolveShell(workflows.Scalar(run.Shell), jobShell, workflowShell, runnerShell).Name
}

func scriptShellVariant(shell string) (shellsyntax.LangVariant, bool) {
	words := strings.Fields(shell)
	if len(words) == 0 || strings.Contains(shell, "${{") {
		return shellsyntax.LangBash, false
	}
	if len(words) > 1 && !directShellScript(words[1:]) {
		return shellsyntax.LangBash, false
	}
	switch path.Base(strings.ReplaceAll(words[0], "\\", "/")) {
	case "bash", "bash.exe":
		return shellsyntax.LangBash, true
	case "sh", "sh.exe":
		return shellsyntax.LangPOSIX, true
	default:
		return shellsyntax.LangBash, false
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
	scalars     map[int]bool
	wordLists   map[int]bool
	environment map[int]scriptEnvironmentReplacement
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
	type span struct {
		first, last int
		environment runners.ContextEnvironment
	}
	var expressions []span
	for offset := 0; offset < len(source); {
		index := strings.Index(source[offset:], "${{")
		if index < 0 {
			break
		}
		first := offset + index
		lexer := syntax.NewLexer(source[first+3:])
		expression, err := syntax.NewParser().ParseExpression(lexer)
		if err != nil {
			return scriptArgumentProofs{}
		}
		last := first + 3 + lexer.Offset()
		expressions = append(expressions, span{first, last, runners.ContextEnvironmentFor(syntax.StaticReference(expression))})
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
	file, err := shellsyntax.NewParser(shellsyntax.Variant(variant)).Parse(strings.NewReader(string(masked)), "")
	if err != nil {
		return scriptArgumentProofs{}
	}
	proofs := scriptArgumentProofs{scalars: map[int]bool{}, wordLists: map[int]bool{}, environment: map[int]scriptEnvironmentReplacement{}}
	shellsyntax.Walk(file, func(node shellsyntax.Node) bool {
		call, ok := node.(*shellsyntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		command := call.Args[0].Lit()
		staticCommand := command != "" && !strings.Contains(source[call.Args[0].Pos().Offset():call.Args[0].End().Offset()], "${{")
		for _, argument := range call.Args[1:] {
			if staticCommand && !scriptEvaluatesArguments(command, call.Args[1:]) {
				for _, part := range argument.Parts {
					quoted, ok := part.(*shellsyntax.DblQuoted)
					if !ok || quoted.Dollar {
						continue
					}
					for _, part := range quoted.Parts {
						literal, ok := part.(*shellsyntax.Lit)
						if !ok {
							continue
						}
						first, last := int(literal.Pos().Offset()), int(literal.End().Offset())
						start := sort.Search(len(expressions), func(i int) bool { return expressions[i].first >= first })
						for _, expression := range expressions[start:] {
							if expression.last > last {
								break
							}
							if expression.environment.Name() != "" && !escapedScriptDollar(source, expression.first) {
								proofs.environment[expression.first] = scriptEnvironmentReplacement{end: expression.last, environment: expression.environment}
							}
						}
					}
				}
			}
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

func wholeLiteralArgument(argument *shellsyntax.Word, expressionFirst, expressionLast int) bool {
	if len(argument.Parts) != 1 {
		return false
	}
	first, last := int(argument.Pos().Offset()), int(argument.End().Offset())
	switch part := argument.Parts[0].(type) {
	case *shellsyntax.Lit:
	case *shellsyntax.SglQuoted:
		if part.Dollar {
			return false
		}
		first, last = first+1, last-1
	case *shellsyntax.DblQuoted:
		if part.Dollar || !literalScriptWord(part.Parts) {
			return false
		}
		first, last = first+1, last-1
	default:
		return false
	}
	return first == expressionFirst && last == expressionLast
}

func literalScriptWord(parts []shellsyntax.WordPart) bool {
	for _, part := range parts {
		switch part := part.(type) {
		case *shellsyntax.Lit:
		case *shellsyntax.SglQuoted:
			if part.Dollar {
				return false
			}
		case *shellsyntax.DblQuoted:
			if part.Dollar || !literalScriptWord(part.Parts) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
