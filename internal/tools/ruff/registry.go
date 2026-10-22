package ruff

import (
	"errors"
	"reflect"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/selection"
)

// Options selects Ruff's transport independently of workflow traversal.
type Options struct {
	Command        string
	Process        *processconfig.Options
	Backend        analyzer.ScriptBackend
	pluginFallback bool
}

func (o Options) forConfig(cfg *config.Config) Options {
	if o.pluginFallback && cfg != nil && cfg.Tools.Ruff.Backend != nil {
		o.Backend = nil
		o.Command = "ruff"
		o.Process = &processconfig.Options{Optional: true}
	}
	return o
}

// Registration owns Ruff selection, transport construction and plugin replacement.
func (o Options) Registration() tools.Registration {
	descriptor, _ := rulecatalog.Find("ruff")
	compatibility := &Compatibility{}
	registration := tools.Registration{
		Descriptor:   descriptor,
		PrepareInput: prepareInput,
		Enabled: func(c tools.Context) bool {
			possible, err := projectMayEnable(c.Config, c.ProjectConfig, c.ProjectRoot != "")
			return (o.Backend != nil || selection.AllowsModule("ruff", o.Command, o.Process) || processconfig.Enabled(o.Command, o.Process)) && (possible || err != nil)
		},
		Required: func(c tools.Context) bool {
			o := o.forConfig(c.Config)
			options := selection.ProcessOptions("ruff", o.Command, o.Process, c.Config, c.WorkingDir)
			return o.Backend != nil || selection.Module("ruff", o.Command, o.Process, c.Config) != nil || options == nil || !options.Optional
		},
		Build: func(c tools.Context) (tools.Runner, error) {
			adapter, err := newRuffAnalyzer(c, o, compatibility)
			if err != nil {
				return nil, err
			}
			return adapter, nil
		},
		NeedsProcess: o.projectMayNeedProcess,
		Validate: func() error {
			if o.Backend != nil && (o.Process != nil || o.Command != "") {
				return errors.New("ruff selects both a backend and a process")
			}
			return nil
		},
		WithBackend: func(backend analyzer.ScriptBackend) (tools.Registration, error) {
			if o.Backend == nil && !processconfig.Enabled(o.Command, o.Process) {
				return o.Registration(), nil
			}
			if o.Backend != nil || o.Process != nil && !reflect.DeepEqual(*o.Process, processconfig.Options{Optional: true}) || o.Command != "" && o.Command != "ruff" {
				return tools.Registration{}, errors.New("ruff plugin conflicts with an explicitly selected Ruff backend or command")
			}
			next := o
			next.Command = ""
			next.Process = nil
			next.Backend = backend
			next.pluginFallback = true
			return next.Registration(), nil
		},
	}
	registration.ConfiguredBackend = func(c tools.Context) (analyzer.ScriptBackend, error) {
		if o.Backend != nil {
			return o.Backend, nil
		}
		return selection.Load("ruff", o.Command, o.Process, c.Config, engineLoader(c))
	}
	return registration
}

func engineLoader(c tools.Context) func(string) (analyzer.ScriptBackend, error) {
	if c.LoadEngine == nil {
		return nil
	}
	return func(path string) (analyzer.ScriptBackend, error) { return c.LoadEngine(c.Processes.Ctx, path) }
}
