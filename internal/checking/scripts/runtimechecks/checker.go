// Package runtimechecks reports source-mapped findings from shared script models.
package runtimechecks

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type Checker struct{ declaration rule.Declaration }

func Errexit() Checker         { return Checker{suspicious.ErrexitStatusDeclaration()} }
func WorkflowCommand() Checker { return Checker{suspicious.WorkflowCommandSyntaxDeclaration()} }

func (c Checker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Descriptor: c.declaration.Clone(), Scope: c.declaration.Scope, SupportsActions: c.declaration.SupportsActions}
}

func (c Checker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
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
			start, startOK := script.Position(position(script.Text, span.Start, false))
			end, endOK := script.Position(position(script.Text, span.End, true))
			if !startOK || !endOK {
				return
			}
			path := script.Path
			if path == "" {
				path = input.Path
			}
			diagnostics = append(diagnostics, results.Diagnostic{Rule: c.declaration.Name, Message: message, Path: path, Start: start, End: end})
		}
		if c.declaration.Name == "errexit-status" {
			for _, span := range scripts.ErrexitStatusReads(script.Text, invocation, script.Interpreters) {
				report(span, "Bash errexit exits if the preceding command fails, so this status read cannot capture that failure. Use an explicit condition or an OR handler if the script must continue")
			}
			continue
		}
		if invocation.Status == commands.Unknown && invocation.Reason == "no interpreter definition matches the command" {
			// The runner recognizes these exact built-in shell names even when
			// the corresponding optional language plugin is not installed.
			switch script.Runtime.Shell {
			case "python", "pwsh", "powershell":
				invocation = commands.Invocation{Status: commands.Valid, Language: script.Runtime.Shell, Input: "file"}
			}
		}
		seen := make(map[scripts.ScriptRange]bool)
		for _, path := range scripts.LiteralStdoutPaths(script.Text, invocation, script.Interpreters) {
			var state commands.WorkflowCommandState
			for _, output := range path {
				if problem := state.WorkflowCommandProblem(output.Text); problem != "" && !seen[output.ScriptRange] {
					report(output.ScriptRange, problem)
					seen[output.ScriptRange] = true
				}
			}
		}
	}
	return diagnostics, nil
}

func position(text string, offset int, exclusive bool) (int, int, bool) {
	prefix := text[:offset]
	return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1, exclusive
}
