package correctness

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

// DeprecatedCommandsRule is the single definition of the deprecated-commands rule.
var DeprecatedCommandsRule = &define.Rule{
	Name:            "deprecated-commands",
	Description:     "Report deprecated set-output, save-state, set-env, and add-path commands",
	Category:        "correctness",
	Subcategory:     "scripts",
	Recommended:     true,
	Help:            "Write values to the corresponding quoted GITHUB_OUTPUT, GITHUB_STATE, GITHUB_ENV or GITHUB_PATH file.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations},
	Script:          checkDeprecatedCommands,
}

// checkDeprecatedCommands reports deprecated workflow commands in declared and invoked scripts.
// It maps each command to its declaring document. A command whose text
// has no source position is reported at the script start without a range.
func checkDeprecatedCommands(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		for _, command := range DeprecatedCommands(script.Text) {
			start, startOK := script.Position(scriptTextPosition(script.Text, command.Start, false))
			end, endOK := script.Position(scriptTextPosition(script.Text, command.End, true))
			if !startOK || !endOK {
				start, end = script.Start, sourcepos.Position{}
			}
			findings = append(findings, results.Diagnostic{
				Message: fmt.Sprintf("workflow command %q was deprecated. use `%s` instead: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions", command.Command, command.Replacement),
				Path:    script.Path,
				Start:   start,
				End:     end,
			})
		}
	}
	return findings, nil
}

func scriptTextPosition(text string, offset int, exclusive bool) (int, int, bool) {
	prefix := text[:offset]
	line := strings.Count(prefix, "\n") + 1
	column := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
	return line, column, exclusive
}
