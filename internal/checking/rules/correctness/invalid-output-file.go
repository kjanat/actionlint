package correctness

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// InvalidOutputFileDeclaration describes the invalid-output-file rule.
func InvalidOutputFileDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "invalid-output-file", Description: "Check literal writes to GITHUB_OUTPUT",
		Category: "correctness", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Recommended: true,
		DefaultLevel: "error",
		Explanation: "GITHUB_OUTPUT requires NAME=value records or multiline records enclosed by NAME<<delimiter and a matching delimiter. " +
			"This check combines literal writes from Bash and sh scripts, then validates the complete file. " +
			"It handles literal echo and printf commands, command groups, and cat or tee with literal text from a here-document or pipe. " +
			"It skips the script if it contains dynamic values, unsupported commands, variable assignments or control flow. " +
			"Commands that could redefine these utilities also cause the script to be skipped.",
		Help: "Write NAME=value for a single-line output. " +
			"For a multiline output, write NAME<<delimiter, the value, and the matching delimiter.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#invalid-output-file",
		SupportsActions:  true, Scope: rule.SourceConfiguration,
		RequiredInputs: []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
		Examples: []rule.Example{rule.WorkflowExample("Give the output a name instead of writing a JSON document directly.",
			"    steps:\n      - shell: bash\n        run: echo '{\"available\":true}' >> \"$GITHUB_OUTPUT\"\n",
			"    steps:\n      - shell: bash\n        run: echo 'available=true' >> \"$GITHUB_OUTPUT\"\n")},
	}
}

// InvalidOutputFileChecker checks output files whose contents can be determined from the script.
type InvalidOutputFileChecker struct{}

func (InvalidOutputFileChecker) Descriptor() checkinputs.Descriptor {
	declaration := InvalidOutputFileDeclaration()
	return checkinputs.Descriptor{Descriptor: declaration.Clone(), Scope: declaration.Scope, SupportsActions: declaration.SupportsActions}
}

func (InvalidOutputFileChecker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var diagnostics []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		if script.Runtime == nil || !script.Runtime.ShellKnown {
			continue
		}
		file, known := scriptmodel.KnownOutputFile(script.Text, script.Runtime.Shell)
		if !known {
			continue
		}
		problem := scriptmodel.NamedFileCommandError(file.Text)
		if problem == "" {
			continue
		}
		start, startOK := script.Position(outputFilePosition(script.Text, file.Start, false))
		end, endOK := script.Position(outputFilePosition(script.Text, file.End, true))
		if !startOK || !endOK {
			continue
		}
		path := script.Path
		if path == "" {
			path = input.Path
		}
		diagnostics = append(diagnostics, results.Diagnostic{
			Rule: "invalid-output-file", Path: path, Start: start, End: end,
			Message: fmt.Sprintf("GITHUB_OUTPUT contents are invalid: %s. Write named outputs using NAME=value or NAME<<delimiter", problem),
		})
	}
	return diagnostics, nil
}

func outputFilePosition(text string, offset int, exclusive bool) (int, int, bool) {
	prefix := text[:offset]
	return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1, exclusive
}
