package expressions

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	runner "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/results"
)

type scriptEnvironmentReplacement struct {
	end         int
	environment runner.ContextEnvironment
}

func escapedScriptDollar(script string, offset int) bool {
	backslashes := 0
	for i := offset - 1; i >= 0 && script[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 != 0
}

// Quoting an argument does not make text safe for another interpreter to parse.
func scriptEvaluatesArguments(command string, arguments []*syntax.Word) bool {
	command = path.Base(command)
	if command == "eval" {
		return true
	}
	switch command {
	case "bash", "sh", "dash", "zsh", "ksh", "node", "bun", "python", "python3", "ruby", "perl":
		for _, argument := range arguments {
			option := argument.Lit()
			switch option {
			case "-c", "-e", "-p", "--eval", "--print", "--command":
				return true
			}
			if strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.Contains(option, "c") {
				return true
			}
		}
	}
	return false
}

func (rule *Checker) addScriptEnvironmentFix(report *report) {
	first := rule.expressionOffset - 3
	replacement, ok := rule.scriptEnvironment[first]
	if !ok || !replacement.environment.Available(rule.compositeScript) {
		return
	}
	start, end := rule.expressionPosition(first, false), rule.expressionPosition(replacement.end, true)
	if start == nil || end == nil {
		return
	}
	edit := results.Edit{
		Start:       sourcepos.Position{Line: start.Line, Column: start.Col},
		End:         sourcepos.Position{Line: end.Line, Column: end.Col},
		Replacement: "${" + replacement.environment.Name() + "}",
	}
	report.fixes = append(report.fixes, results.Fix{
		Description:   "Use the existing " + replacement.environment.Name() + " environment variable",
		Applicability: results.FixSuggested,
		Edits:         []results.Edit{edit},
	})
}
