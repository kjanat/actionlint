package correctness

import (
	"context"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

var InvalidOutputFileRule = &define.Rule{
	Name:            "invalid-output-file",
	Description:     "Check literal writes to GITHUB_OUTPUT",
	Category:        "correctness",
	Subcategory:     "scripts",
	Recommended:     true,
	DefaultLevel:    "error",
	Explanation:     "Report literal writes from Bash and sh scripts that make GITHUB_OUTPUT invalid.",
	Help:            "Write NAME=value. For a multiline value, write NAME<<delimiter, the value, and the same delimiter.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
	Examples: []rule.Example{rule.WorkflowExample("Name the output.",
		"    steps:\n      - shell: bash\n        run: echo '{\"available\":true}' >> \"$GITHUB_OUTPUT\"\n",
		"    steps:\n      - shell: bash\n        run: echo 'available=true' >> \"$GITHUB_OUTPUT\"\n")},
	Script: checkInvalidOutputFile,
}

func checkInvalidOutputFile(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	var diagnostics []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		if script.Runtime == nil || !script.Runtime.Shell.Known() {
			continue
		}
		file, known := scripts.KnownOutputFile(script.Text, script.Runtime.Shell.Name)
		if !known {
			continue
		}
		problem := scripts.NamedFileCommandError(file.Text)
		if problem == "" {
			continue
		}
		start, startOK := script.OffsetPosition(file.Start, false)
		end, endOK := script.OffsetPosition(file.End, true)
		if !startOK || !endOK {
			continue
		}
		path := script.Path
		if path == "" {
			path = input.Path
		}
		diagnostics = append(diagnostics, results.Diagnostic{
			Path: path, Start: start, End: end,
			Message: fmt.Sprintf("GITHUB_OUTPUT contents are invalid: %s. Write named outputs using NAME=value or NAME<<delimiter", problem),
		})
	}
	return diagnostics, nil
}
