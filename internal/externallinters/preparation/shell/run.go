package shell

import (
	"context"
	"errors"

	"actionlint.kjanat.dev/v2/internal/externallinters/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation/mask"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

var ErrNoSourceAccess = errors.New("a snippet engine does not provide external source access")

type Run struct {
	Shell         Shell
	Settings      *integrationconfig.ResolvedSettings
	SourcePlanner *preparation.SourcePlanner
	ConfigFile    string
	Sources       bool
	Engine        bool
	Input         bool
	Language      string
	Environment   []scripts.EnvironmentNames
}

func PrepareRun(ctx context.Context, r Run, text string) (preparation.Prepared, error) {
	language, setup := r.Language, ""
	if !r.Input {
		invocation := r.Shell.Invocation()
		if invocation.Status == commands.Invalid {
			return preparation.Prepared{Skip: "invalid interpreter invocation: " + invocation.Issues[0].Message}, nil
		}
		language, setup = r.Shell.Analysis()
	}
	settings := integrationconfig.ResolvedSettings{}
	if r.Settings != nil {
		settings = *r.Settings
	}
	if r.Engine && (len(settings.ReadAccess.Directories) != 0 || settings.ReadAccess.Enabled != nil && *settings.ReadAccess.Enabled) {
		return preparation.Prepared{}, ErrNoSourceAccess
	}
	declarations, known := EnvironmentStartup("", r.Environment)
	plan, err := r.SourcePlanner.Plan(ctx, preparation.SourcePlanInput{
		Source: text, Language: language, Startup: setup,
		EnvironmentStartup: declarations, EnvironmentKnown: known,
		Settings: settings, ConfigFile: r.ConfigFile,
		Filesystem: r.Sources, Engine: r.Engine, Input: r.Input,
	})
	if err != nil {
		return preparation.Prepared{}, err
	}
	if plan.Skip != "" {
		return preparation.Prepared{Skip: plan.Skip}, nil
	}
	script := Prepare(mask.Expressions(text), plan.Prefix, plan.Startup)
	prepared := preparation.Prepared{Source: script.Text(), LineMap: script.Line, Config: plan.Config, Vars: plan.Variables, Excluded: plan.Excluded, ConfigFile: r.ConfigFile}
	for _, variant := range mask.Variants(text) {
		prepared.Variants = append(prepared.Variants, preparation.Variant{Source: script.Substitute(variant.Text), Substitutions: variant.Substitutions})
	}
	return prepared, nil
}
