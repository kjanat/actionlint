package correctness

import (
	"context"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

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

// A command without a source position is reported at the script start without a range.
func checkDeprecatedCommands(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		for _, command := range DeprecatedCommands(script.Text) {
			start, startOK := script.OffsetPosition(command.Start, false)
			end, endOK := script.OffsetPosition(command.End, true)
			if !startOK || !endOK {
				start, end = script.Start, sourcetext.Position{}
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
