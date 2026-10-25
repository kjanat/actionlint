package shellcheck

import (
	"context"
	"errors"
	"reflect"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/selection"
)

// Options selects ShellCheck's transport and source configuration.
type Options struct {
	// ManagedOnly permits a declared analyzer without authorizing native fallback.
	ManagedOnly bool
	Command     string
	Process     *processconfig.Options
	Backend     analyzer.ScriptBackend
	Runner      func([]string, string) ([]byte, error)
	Settings    *toolconfig.ShellcheckSettings
}

// Registration owns ShellCheck selection, transport construction and plugin replacement.
func (o Options) Registration() tools.Registration {
	descriptor, _ := rulecatalog.Find("shellcheck")
	registration := tools.Registration{
		Descriptor: descriptor, RequiresExecution: true, PrepareInput: prepareInput,
		Enabled: func(c tools.Context) bool {
			return config.DiagnosticLevel(c.Config, "shellcheck") != "off" && (o.Backend != nil || o.Runner != nil || selection.AllowsModule("shellcheck", o.Command, o.Process) || processconfig.Enabled(o.Command, o.Process)) && (c.Config == nil || c.Config.Tools.Shellcheck.Enabled == nil || *c.Config.Tools.Shellcheck.Enabled)
		},
		Required: func(c tools.Context) bool {
			options := selection.ProcessOptions("shellcheck", o.Command, o.Process, c.Config, c.WorkingDir)
			return o.Backend != nil || selection.Module("shellcheck", o.Command, o.Process, c.Config) != nil || options != nil && !options.Optional
		},
		Build: func(c tools.Context) (tools.Runner, error) {
			var load func(string) (analyzer.ScriptBackend, error)
			if c.LoadEngine != nil {
				load = func(path string) (analyzer.ScriptBackend, error) { return c.LoadEngine(c.Processes.Ctx, path) }
			}
			debug := c.Debug
			if debug == nil {
				debug = func(string, ...any) {}
			}
			adapter, err := newShellcheckAnalyzer(adapterContext{allowConfigProcesses: c.AllowConfigProcesses, config: c.Config, workingDir: c.WorkingDir, process: c.Processes, debug: debug, scriptEngine: load, shellcheck: o.Command, shellcheckOptions: o.Process, shellcheckBackend: o.Backend, shellcheckRunner: o.Runner, shellcheckSettings: o.Settings})
			if err != nil {
				return nil, err
			}
			return runner{adapter}, nil
		},
		NeedsProcess: func(_ context.Context, cfg, project *config.Config, inside bool) (bool, error) {
			// ShellCheck skips the caller's composites when its rule or tool is off.
			if o.Backend != nil || o.Runner != nil || config.DiagnosticLevel(cfg, "shellcheck") == "off" || cfg != nil && cfg.Tools.Shellcheck.Enabled != nil && !*cfg.Tools.Shellcheck.Enabled {
				return false, nil
			}
			return selection.ProjectMayNeedProcess("shellcheck", cfg, project, inside, o.Command, o.Process)
		},
		Validate: func() error {
			if o.Backend != nil && (o.Process != nil || o.Command != "" || o.Runner != nil || o.Settings != nil) {
				return errors.New("shellcheck selects both a backend and process settings")
			}
			return nil
		},
		WithBackend: func(backend analyzer.ScriptBackend) (tools.Registration, error) {
			if o.Backend == nil && o.Runner == nil && !processconfig.Enabled(o.Command, o.Process) {
				return o.Registration(), nil
			}
			if o.Backend != nil || o.Process != nil && !reflect.DeepEqual(*o.Process, processconfig.Options{Optional: true}) || o.Command != "" && o.Command != "shellcheck" || o.Runner != nil || o.Settings != nil {
				return tools.Registration{}, errors.New("shellcheck plugin conflicts with an explicitly selected backend or command")
			}
			next := o
			next.Command = ""
			next.Process = nil
			next.Backend = backend
			return next.Registration(), nil
		},
		WithAnalyzer: o.withAnalyzer,
	}
	registration.ConfiguredBackend = func(c tools.Context) (analyzer.ScriptBackend, error) {
		if o.Backend != nil {
			return o.Backend, nil
		}
		var load func(string) (analyzer.ScriptBackend, error)
		if c.LoadEngine != nil {
			load = func(path string) (analyzer.ScriptBackend, error) { return c.LoadEngine(c.Processes.Ctx, path) }
		}
		return selection.Load("shellcheck", o.Command, o.Process, c.Config, load)
	}
	return registration
}
