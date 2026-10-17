package adapters

import (
	"errors"
	"fmt"
	"reflect"

	"actionlint.kjanat.dev/analyzer"
	config "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/ruff"
	"actionlint.kjanat.dev/internal/rulecatalog"
	"actionlint.kjanat.dev/internal/tools"
)

// Options is compatibility input for the bundled adapters, not a host contract.
type Options struct {
	ShellCheck, Ruff               string
	ShellcheckOptions, RuffOptions *processconfig.Options
	ShellcheckBackend, RuffBackend analyzer.ScriptBackend
	ShellcheckRunner               func([]string, string) ([]byte, error)
	ShellcheckSettings             *ShellcheckSettings
}

func (o Options) Registrations() []tools.Registration {
	compatibility := &ruff.Compatibility{}
	contextOf := func(c tools.Context) ruleContext {
		var load func(string) (analyzer.ScriptBackend, error)
		if c.LoadEngine != nil {
			load = func(path string) (analyzer.ScriptBackend, error) { return c.LoadEngine(c.Processes.Ctx, path) }
		}
		debug := c.Debug
		if debug == nil {
			debug = func(string, ...any) {}
		}
		return ruleContext{config: c.Config, projectConfig: c.ProjectConfig, workingDir: c.WorkingDir, projectRoot: c.ProjectRoot, process: c.Processes, debug: debug, scriptEngine: load, ruff: o.Ruff, ruffOptions: o.RuffOptions, ruffBackend: o.RuffBackend, ruffCompatibility: compatibility, ruffWarning: func(err error) {
			if c.Warn != nil {
				c.Warn(fmt.Errorf("skipping automatically discovered Ruff: %w", err))
			}
		}, shellcheck: o.ShellCheck, shellcheckOptions: o.ShellcheckOptions, shellcheckBackend: o.ShellcheckBackend, shellcheckRunner: o.ShellcheckRunner, shellcheckSettings: o.ShellcheckSettings}
	}
	makeRegistration := func(name string, execution bool, build func(ruleContext) (scriptAnalyzer, error), enabled, required func(ruleContext) bool) tools.Registration {
		descriptor, _ := rulecatalog.Find(name)
		return tools.Registration{Descriptor: descriptor, RequiresExecution: execution,
			Enabled:  func(c tools.Context) bool { return enabled(contextOf(c)) },
			Required: func(c tools.Context) bool { return required(contextOf(c)) },
			Build: func(c tools.Context) (tools.Runner, error) {
				a, err := build(contextOf(c))
				if err != nil {
					return nil, err
				}
				return runner{a}, nil
			},
		}
	}
	rr := makeRegistration("ruff", false, newRuffAnalyzer, func(c ruleContext) bool {
		possible, err := config.RuffProjectMayEnable(c.config, c.projectConfig, c.projectRoot != "")
		return (c.ruffBackend != nil || externalCommandEnabled(c.ruff, c.ruffOptions)) && (possible || err != nil)
	}, func(c ruleContext) bool {
		options := projectToolOptions("ruff", c.ruff, c.ruffOptions, c.config, c.workingDir)
		return c.ruffBackend != nil || configuredScriptModule("ruff", c.ruff, c.ruffOptions, c.config) != nil || options == nil || !options.Optional
	})
	rr.NeedsProcess = func(cfg, project *Config, inside bool) (bool, error) {
		possible, err := config.RuffProjectMayEnable(cfg, project, inside)
		return o.RuffBackend == nil && configuredScriptModule("ruff", o.Ruff, o.RuffOptions, cfg) == nil && externalCommandEnabled(o.Ruff, o.RuffOptions) && possible, err
	}
	rr.Validate = func() error {
		if o.RuffBackend != nil && (o.RuffOptions != nil || o.Ruff != "") {
			return errors.New("ruff selects both a backend and a process")
		}
		return nil
	}
	rr.WithBackend = func(backend analyzer.ScriptBackend) (tools.Registration, error) {
		if o.RuffBackend != nil || o.RuffOptions != nil && !reflect.DeepEqual(*o.RuffOptions, processconfig.Options{Optional: true}) || (o.Ruff != "" && o.Ruff != "ruff") {
			return tools.Registration{}, errors.New("ruff plugin conflicts with an explicitly selected Ruff backend or command")
		}
		next := o
		next.Ruff = ""
		next.RuffOptions = nil
		next.RuffBackend = backend
		return next.Registrations()[0], nil
	}
	sc := makeRegistration("shellcheck", true, newShellcheckAnalyzer, func(c ruleContext) bool {
		return diagnosticLevel(c.config, "shellcheck") != "off" && (c.shellcheckBackend != nil || c.shellcheckRunner != nil || externalCommandEnabled(c.shellcheck, c.shellcheckOptions)) && (c.config == nil || c.config.Tools.Shellcheck.Enabled == nil || *c.config.Tools.Shellcheck.Enabled)
	}, func(c ruleContext) bool {
		options := projectToolOptions("shellcheck", c.shellcheck, c.shellcheckOptions, c.config, c.workingDir)
		return c.shellcheckBackend != nil || configuredScriptModule("shellcheck", c.shellcheck, c.shellcheckOptions, c.config) != nil || options != nil && !options.Optional
	})
	sc.NeedsProcess = func(cfg, _ *Config, _ bool) (bool, error) {
		return o.ShellcheckBackend == nil && o.ShellcheckRunner == nil && configuredScriptModule("shellcheck", o.ShellCheck, o.ShellcheckOptions, cfg) == nil && externalCommandEnabled(o.ShellCheck, o.ShellcheckOptions) && diagnosticLevel(cfg, "shellcheck") != "off" && (cfg == nil || cfg.Tools.Shellcheck.Enabled == nil || *cfg.Tools.Shellcheck.Enabled), nil
	}
	sc.Validate = func() error {
		if o.ShellcheckBackend != nil && (o.ShellcheckOptions != nil || o.ShellCheck != "" || o.ShellcheckRunner != nil || o.ShellcheckSettings != nil) {
			return errors.New("shellcheck selects both a backend and process settings")
		}
		return nil
	}
	sc.WithBackend = func(backend analyzer.ScriptBackend) (tools.Registration, error) {
		if o.ShellcheckBackend != nil || o.ShellcheckOptions != nil && !reflect.DeepEqual(*o.ShellcheckOptions, processconfig.Options{Optional: true}) || (o.ShellCheck != "" && o.ShellCheck != "shellcheck") || o.ShellcheckRunner != nil || o.ShellcheckSettings != nil {
			return tools.Registration{}, errors.New("shellcheck plugin conflicts with an explicitly selected backend or command")
		}
		next := o
		next.ShellCheck = ""
		next.ShellcheckOptions = nil
		next.ShellcheckBackend = backend
		return next.Registrations()[1], nil
	}
	return []tools.Registration{rr, sc}
}
