package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Checker reports deprecated workflow commands in declared and invoked scripts
// through the public analyzer contract.
type Checker struct{}

// Descriptor names the rule and the facts it consumes.
func (Checker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "deprecated-commands", RequiredInputs: []checkinputs.InputKind{checkinputs.Scripts, checkinputs.Invocations}}
}

// CheckDocument maps each command to its declaring document. A command whose text
// has no source position is reported at the script start without a range.
func (Checker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		for _, command := range Deprecated(script.Text) {
			start, startOK := script.Position(textPosition(script.Text, command.Start, false))
			end, endOK := script.Position(textPosition(script.Text, command.End, true))
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

func textPosition(text string, offset int, exclusive bool) (int, int, bool) {
	prefix := text[:offset]
	line := strings.Count(prefix, "\n") + 1
	column := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
	return line, column, exclusive
}
