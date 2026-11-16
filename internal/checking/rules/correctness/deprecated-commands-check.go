package correctness

import (
	"context"
	"fmt"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

// DeprecatedCommandsChecker reports deprecated workflow commands in declared and invoked scripts
// through the public analyzer contract.
type DeprecatedCommandsChecker struct{}

// Descriptor names the rule and the facts it consumes.
func (DeprecatedCommandsChecker) Descriptor() checkinputs.Descriptor {
	declaration := DeprecatedCommandsDeclaration()
	return checkinputs.Descriptor{Descriptor: declaration.Clone(), Scope: declaration.Scope, SupportsActions: declaration.SupportsActions}
}

// CheckDocument maps each command to its declaring document. A command whose text
// has no source position is reported at the script start without a range.
func (DeprecatedCommandsChecker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		for _, command := range DeprecatedCommands(script.Text) {
			start, startOK := script.OffsetPosition(command.Start, false)
			end, endOK := script.OffsetPosition(command.End, true)
			if !startOK || !endOK {
				start, end = script.Start, sourcepos.Position{}
			}
			findings = append(findings, results.Diagnostic{
				Rule:    "deprecated-commands",
				Message: fmt.Sprintf("workflow command %q was deprecated. use `%s` instead: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions", command.Command, command.Replacement),
				Path:    script.Path,
				Start:   start,
				End:     end,
			})
		}
	}
	return findings, nil
}
