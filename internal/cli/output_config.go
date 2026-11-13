package cli

import (
	"os"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
)

// applyConfiguredOutput uses the invoking directory's configuration for the
// whole command. Configurations in scanned repositories only affect their checks.
func (a *commandApp) applyConfiguredOutput() error {
	if a.inv.Operation != operationCheck || a.inv.Check.Config.Disabled {
		return nil
	}
	path, err := config.SelectedPath(a.inv.Check.Config)
	if err != nil || path == "" {
		return err
	}
	reader := configsource.New(a.ctx, nil, a.streams.Acquisition.Connections)
	loaded, err := checking.ConfigurationRules().ReadBeforePluginsContext(a.ctx, path, reader.ReadFile)
	if err != nil {
		return err
	}
	settings := loaded.Config.Output
	if !a.formatSelected() && !a.prettyJSONSelected() && !a.inv.JSON && a.inv.Render.Template == "" && a.inv.Render.TemplateFile == "" && len(settings.Formats) != 0 {
		a.opts.output = outputModes(settings.Formats)
		a.set["output-format"] = true
	}
	if settings.LogLevel != nil && !a.anySet("log-level", "verbose", "v", "debug") {
		a.opts.logLevel = *settings.LogLevel
		a.set["log-level"] = true
	}
	if settings.Summary != nil && !a.configOutputOverridden([]string{"summary"}, "ACTIONLINT_SUMMARY") {
		a.inv.Render.Summary = *settings.Summary
	}
	if settings.NetworkStats != nil && !a.set["network-stats"] {
		a.opts.networkStats = *settings.NetworkStats
	}
	if settings.Quiet != nil && !a.configOutputOverridden([]string{"quiet", "q"}, "ACTIONLINT_QUIET") {
		a.inv.Render.Quiet = *settings.Quiet
	}
	return nil
}

func (a *commandApp) configOutputOverridden(flags []string, environment ...string) bool {
	if a.anySet(flags...) {
		return true
	}
	for _, name := range environment {
		if _, set := os.LookupEnv(name); set {
			return true
		}
	}
	return false
}
