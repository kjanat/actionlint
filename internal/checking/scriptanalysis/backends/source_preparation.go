package backends

import (
	"context"
	"maps"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/preparation/shell"
)

func applySourcePreparation(ctx context.Context, transform *preparation.SourcePlanner, script scriptanalysis.Unit, prepared preparation.Prepared, settings integrationconfig.ResolvedSettings, via route) (preparation.Prepared, error) {
	if transform == nil || prepared.Skip != "" {
		return prepared, nil
	}
	language := scriptShell(script)
	if script.Input != nil {
		language = script.Input.Site.Language
	}
	declarations, known := shell.EnvironmentStartup("", script.Environment)
	switch language {
	case "bash", "sh", "dash", "ksh", "busybox":
	default:
		declarations = ""
	}
	_, directoryKnown := script.WorkingDirectory()
	plan, err := transform.Plan(ctx, preparation.SourcePlanInput{
		Source: script.Text(), Language: language,
		EnvironmentStartup: declarations, EnvironmentKnown: known,
		Settings:   settings,
		Filesystem: via == routeProcess && directoryKnown,
		Engine:     via == routeEngine, Input: script.Input != nil,
	})
	if err != nil {
		return preparation.Prepared{}, err
	}
	if plan.Skip != "" {
		return preparation.Prepared{Skip: plan.Skip}, nil
	}
	prepared.Config = plan.Config
	if plan.Variables != nil {
		if prepared.Vars == nil {
			prepared.Vars = map[string]string{}
		}
		maps.Copy(prepared.Vars, plan.Variables)
	}
	if plan.Excluded != nil {
		prepared.Excluded = plan.Excluded
	}
	if plan.Prefix == "" && plan.Startup == "" {
		return prepared, nil
	}
	mapped := shell.Prepare(prepared.Source, plan.Prefix, plan.Startup)
	previousMap := prepared.LineMap
	prepared.Source = mapped.Text()
	prepared.LineMap = func(row int) preparation.Line {
		line := mapped.Line(row)
		if line.Origin == preparation.LineScript && previousMap != nil {
			return previousMap(line.Line)
		}
		return line
	}
	if previousFilter := prepared.AcceptDiagnostic; previousFilter != nil {
		prepared.AcceptDiagnostic = func(diagnostic checkinputs.ScriptDiagnostic) bool {
			line := mapped.Line(diagnostic.Location.Row)
			if line.Origin != preparation.LineScript {
				return true
			}
			diagnostic.Location.Row = line.Line
			diagnostic.EndLocation.Row = mapped.OriginalLine(diagnostic.EndLocation.Row)
			return previousFilter(diagnostic)
		}
	}
	return prepared, nil
}
