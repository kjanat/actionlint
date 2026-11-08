package shell

import (
	"context"
	"errors"

	"actionlint.kjanat.dev/v2/internal/model/commands"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
)

var ErrNoSourceAccess = errors.New("a snippet engine does not provide external source access")

// Run supplies interpreter facts and integration settings for one script.
type Run struct {
	Shell       Shell
	Settings    *integrationconfig.Settings
	Transform   *preparation.SourceTransform
	ConfigFile  string
	Sources     bool
	Engine      bool
	Input       bool
	Language    string
	Environment []scriptmodel.EnvironmentNames
}

// PrepareRun validates the invocation, applies an integration's preparation plan
// and maps the generated rows back to the original script.
func PrepareRun(ctx context.Context, r Run, text string) (preparation.Prepared, error) {
	language, setup := r.Language, ""
	if !r.Input {
		invocation := r.Shell.Invocation()
		if invocation.Status == commands.Invalid {
			return preparation.Prepared{Skip: "invalid interpreter invocation: " + invocation.Issues[0].Message}, nil
		}
		language, setup = r.Shell.Analysis()
	}
	settings := integrationconfig.Settings{}
	if r.Settings != nil {
		settings = *r.Settings
	}
	if r.Engine && (len(settings.ReadAccess.Directories) != 0 || settings.ReadAccess.Enabled != nil && *settings.ReadAccess.Enabled) {
		return preparation.Prepared{}, ErrNoSourceAccess
	}
	declarations, known := EnvironmentStartup("", r.Environment)
	plan, err := r.Transform.Apply(ctx, preparation.ScriptContext{
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
	return preparation.Prepared{Source: script.Text(), LineMap: script.Line, Config: plan.Config, Vars: plan.Variables, Excluded: plan.Excluded}, nil
}
