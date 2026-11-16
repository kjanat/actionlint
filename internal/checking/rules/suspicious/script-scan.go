package suspicious

import (
	"context"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// scanRuntimeScripts runs scan over every declared and invoked script whose
// shell is known and which the shell would execute. Findings carry the source
// range scan reports; a script without a mappable range yields no finding.
func scanRuntimeScripts(
	ctx context.Context, input checkinputs.AnalyzerInput,
	scan func(script checkinputs.Script, invocation commands.Invocation, report func(scripts.ScriptRange, string)),
) ([]results.Diagnostic, error) {
	var diagnostics []results.Diagnostic
	for _, script := range slices.Concat(input.Scripts, input.Invoked) {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		if script.Runtime == nil || !script.Runtime.ShellKnown || script.Interpreters == nil {
			continue
		}
		invocation := script.Interpreters.Analyze(script.Runtime.Shell, script.Runtime.ShellImplicit)
		noexec, onecmd := false, false
		for _, setting := range invocation.Settings {
			if setting.Group == "set" && setting.Name == "noexec" {
				noexec = setting.Enabled
			}
			if setting.Group == "set" && setting.Name == "onecmd" {
				onecmd = setting.Enabled
			}
		}
		if noexec || onecmd {
			continue
		}
		report := func(span scripts.ScriptRange, message string) {
			start, startOK := script.OffsetPosition(span.Start, false)
			end, endOK := script.OffsetPosition(span.End, true)
			if !startOK || !endOK {
				return
			}
			path := script.Path
			if path == "" {
				path = input.Path
			}
			diagnostics = append(diagnostics, results.Diagnostic{Message: message, Path: path, Start: start, End: end})
		}
		scan(script, invocation, report)
	}
	return diagnostics, nil
}
