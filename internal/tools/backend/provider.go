package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
	"actionlint.kjanat.dev/v2/internal/tools/selection"
)

// ProcessProvider selects how a process tool provider runs.
type ProcessProvider struct {
	Manifest provider.Manifest
	// Programs resolves the manifest's transform digests.
	Programs func(digest string) ([]byte, bool)
	// Command replaces the manifest entry and adds user arguments and
	// environment. Nil runs the entry found on PATH, or the executable
	// tools.<name>.backend.process names when the invocation allows
	// repository-selected processes.
	Command *processconfig.Options
	// Required makes a failed build or version probe an error. Explicit
	// selections are required; defaults are skipped with a warning.
	Required bool
}

// RegisterProcess registers a script tool provider from its process
// manifest. The manifest's shell and action-input selectors choose scripts,
// its capabilities admit fixes and reported sources and format messages, and
// tools.<name>.config reaches the engine as its configuration. Scripts reach
// the engine with expressions masked.
func RegisterProcess(p ProcessProvider) (tools.Registration, error) {
	m := p.Manifest
	if m.Role != provider.RoleScript || m.Transport != provider.TransportProcess || m.Process == nil {
		return tools.Registration{}, fmt.Errorf("tool provider %s is not a process script provider", m.Rule.Name)
	}
	name := m.Rule.Name
	descriptor := analyzer.ScriptEngineDescriptor{Rule: m.Rule, Shells: m.Selectors.Shells, ScriptInputs: m.Selectors.Inputs, TemplateContract: m.Capabilities.TemplateContract}.Clone()
	if len(descriptor.Shells)+len(descriptor.ScriptInputs) == 0 {
		return tools.Registration{}, fmt.Errorf("tool provider %s selects no scripts; declare selectors.shells or selectors.inputs", name)
	}
	if _, err := shellCatalogue(descriptor.Rule, descriptor.Shells); err != nil {
		return tools.Registration{}, err
	}
	if err := analyzer.ValidateScriptInputs(descriptor.ScriptInputs); err != nil {
		return tools.Registration{}, err
	}
	enabled := func(c tools.Context) bool { return toolEnabled(c.Config, name) }
	options := engineOptions{
		enabled:  enabled,
		prepare:  func(script tools.Script) (tools.PreparedInput, error) { return configuredInput(name, script) },
		filename: m.Selectors.Filename,
		findings: findingCapabilities{fixes: m.Capabilities.Fixes, reportedSources: m.Capabilities.ReportedSources, messageTemplate: m.Capabilities.MessageTemplate},
	}
	return tools.Registration{
		Descriptor:        descriptor.Rule,
		ScriptInputs:      slices.Clone(descriptor.ScriptInputs),
		RequiresExecution: len(descriptor.Shells) != 0 || len(m.Capabilities.Resources) != 0,
		Enabled:           enabled,
		Required:          func(tools.Context) bool { return p.Required },
		NeedsProcess: func(_ context.Context, cfg, project *config.Config, inside bool) (bool, error) {
			if !toolEnabled(cfg, name) {
				return false, nil
			}
			return selection.ProjectMayEnable(name, cfg, project, inside)
		},
		Build: func(c tools.Context) (tools.Runner, error) {
			if c.Processes == nil {
				return nil, fmt.Errorf("tool provider %s needs a process pool", name)
			}
			command := p.Command
			if command == nil {
				if err := selection.RequireProcessOptIn(name, name, nil, c.Config, c.AllowConfigProcesses); err != nil {
					return nil, err
				}
				command = selection.ProcessOptions(name, name, nil, c.Config, c.WorkingDir)
			}
			if !processconfig.Enabled(m.Process.Entry, command) {
				return nil, fmt.Errorf("tools.%s: no executable is selected", name)
			}
			resolved, err := c.Processes.ConfiguredCommandRunner(m.Process.Entry, command, false)
			if err != nil {
				return nil, fmt.Errorf("tools.%s: %w", name, err)
			}
			backend, err := process.NewBackend(m, resolved, process.Options{Programs: p.Programs, Required: p.Required, Warn: c.Warn, Debug: c.Debug})
			if err != nil {
				return nil, err
			}
			engine, err := registerEngine(descriptor, backend, options)
			if err != nil {
				return nil, err
			}
			return engine.Build(c)
		},
	}, nil
}

// toolEnabled reports whether configuration leaves a tool on: its rule is
// not off and tools.<name>.enabled is not false.
func toolEnabled(cfg *config.Config, name string) bool {
	if cfg == nil {
		return true
	}
	enabled := cfg.Tools[name].Enabled
	return config.DiagnosticLevel(cfg, name) != "off" && (enabled == nil || *enabled)
}

// configuredInput masks expressions and passes inline tools.<name>.config
// settings to the engine.
func configuredInput(name string, script tools.Script) (tools.PreparedInput, error) {
	prepared := tools.PreparedInput{Source: maskExpressions(script.Text)}
	if script.Config == nil {
		return prepared, nil
	}
	settings := script.Config.Tools[name].Config
	if settings.Path() != "" {
		return tools.PreparedInput{}, fmt.Errorf("tools.%s.config: %w", name, errConfigFilePath)
	}
	if values := settings.Inline(); values != nil {
		data, err := json.Marshal(values)
		if err != nil {
			return tools.PreparedInput{}, fmt.Errorf("tools.%s.config: %w", name, err)
		}
		prepared.Config = data
	}
	return prepared, nil
}

// errConfigFilePath rejects a configuration file for a provider registered
// from a manifest, which receives only inline settings.
var errConfigFilePath = errors.New("a tool provider registered from a manifest takes inline settings, not a configuration file path")
