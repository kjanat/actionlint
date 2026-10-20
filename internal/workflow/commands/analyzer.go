package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

// Analyzer reports deprecated workflow commands in declared and invoked scripts
// through the public analyzer contract.
type Analyzer struct{}

// Descriptor names the rule and the facts it consumes.
func (Analyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "deprecated-commands", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations}}
}

// Analyze maps each command to its declaring document. A command whose text
// has no source position is reported at the script start without a range.
func (Analyzer) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	var findings []diagnostic.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		for _, command := range Deprecated(script.Text) {
			start, startOK := script.Position(textPosition(script.Text, command.Start, false))
			end, endOK := script.Position(textPosition(script.Text, command.End, true))
			if !startOK || !endOK {
				start, end = script.Start, diagnostic.Position{}
			}
			findings = append(findings, diagnostic.Diagnostic{
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
