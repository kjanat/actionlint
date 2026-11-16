package correctness

import (
	"context"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// InvalidOutputFileRule is the single definition of the invalid-output-file rule.
var InvalidOutputFileRule = &define.Rule{
	Name:         "invalid-output-file",
	Description:  "Check literal writes to GITHUB_OUTPUT",
	Category:     "correctness",
	Subcategory:  "scripts",
	Recommended:  true,
	DefaultLevel: "error",
	Explanation: "GITHUB_OUTPUT requires NAME=value records or multiline records enclosed by NAME<<delimiter and a matching delimiter. " +
		"This check combines literal writes from Bash and sh scripts, then validates the complete file. " +
		"It handles literal echo and printf commands, command groups, and cat or tee with literal text from a here-document or pipe. " +
		"It skips the script if it contains dynamic values, unsupported commands, variable assignments or control flow. " +
		"Commands that could redefine these utilities also cause the script to be skipped.",
	Help: "Write NAME=value for a single-line output. " +
		"For a multiline output, write NAME<<delimiter, the value, and the matching delimiter.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
	Examples: []rule.Example{rule.WorkflowExample("Write the value as a named output.",
		"    steps:\n      - shell: bash\n        run: echo '{\"available\":true}' >> \"$GITHUB_OUTPUT\"\n",
		"    steps:\n      - shell: bash\n        run: echo 'available=true' >> \"$GITHUB_OUTPUT\"\n")},
	Script: checkInvalidOutputFile,
}

// checkInvalidOutputFile checks output files whose contents can be determined from the script.
func checkInvalidOutputFile(ctx context.Context, input inputs.AnalyzerInput) ([]results.Diagnostic, error) {
	var diagnostics []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		if script.Runtime == nil || !script.Runtime.ShellKnown {
			continue
		}
		file, known := scripts.KnownOutputFile(script.Text, script.Runtime.Shell)
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
