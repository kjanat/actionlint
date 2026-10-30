package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
)

// Selection chooses how a tool provider runs. The zero value disables it.
type Selection struct {
	// Command is the process command line. The provider's rule name selects
	// the manifest's entry on PATH and lets repository configuration choose
	// a backend; empty disables the process unless Process names an
	// executable.
	Command string
	// Process adds arguments and environment to Command, or replaces it with
	// Process.Executable. Optional makes the selection a default that is
	// skipped, with a warning, when it cannot run.
	Process *processconfig.Options
	// Backend replaces the process with a snippet engine.
	Backend analyzer.ScriptBackend
	// Runner replaces the process with an in-process function that has no
	// filesystem, such as an embedding program's ShellCheck build.
	Runner func(args []string, stdin string) ([]byte, error)
	// ManagedOnly permits a managed document analyzer without the native
	// fallback.
	ManagedOnly bool
}

// Provider is a manifest bound to how it runs.
type Provider struct {
	Manifest provider.Manifest
	// Programs resolves the manifest's transform digests.
	Programs  provider.Programs
	Selection Selection
	// Bind completes a registration, such as with a managed document
	// analyzer route; nil leaves it as is. RegisterProvider applies it to every
	// registration it derives.
	Bind func(tools.Registration, Provider) tools.Registration
}

// selection is a Selection as a registration applies it.
type selection struct {
	Selection
	// fallback marks Backend as a default plugin, which a repository's
	// tools.<name>.backend replaces.
	fallback bool
}

// defaultCommand reports whether the selection runs the provider's own
// default command: its rule name with no explicit choice beyond being
// optional.
func (s selection) defaultCommand(name string) bool {
	return s.Backend == nil && s.Runner == nil && s.Command == name &&
		(s.Process == nil || reflect.DeepEqual(*s.Process, processconfig.Options{Optional: true}))
}

// forConfig lets a repository backend replace a default plugin.
func (s selection) forConfig(name string, cfg *config.Config) Selection {
	if s.fallback && cfg != nil && cfg.Tools[name].Backend != nil {
		s.Backend, s.Command, s.Process = nil, name, &processconfig.Options{Optional: true}
	}
	return s.Selection
}

// selected reports whether the selection can run the provider at all.
func (s selection) selected(name string) bool {
	return s.Backend != nil || s.Runner != nil || AllowsModule(name, s.Command, s.Process) || processconfig.Enabled(s.Command, s.Process)
}

// RegisterProvider registers a script tool provider from its process manifest and
// selection. The manifest's preparation profile selects and prepares
// scripts, its selectors route shells and action inputs, its capabilities
// admit fixes and reported sources, format messages and map severities, and
// tools.<name> configures the tool. A repository's tools.<name>.backend may
// select a WebAssembly engine or, with --allow-config-processes, another
// process for the default command.
func RegisterProvider(p Provider) (tools.Registration, error) {
	return register(p, selection{Selection: p.Selection})
}

func register(p Provider, sel selection) (tools.Registration, error) {
	m := p.Manifest
	name := m.Rule.Name
	if m.Role != provider.RoleScript || m.Transport != provider.TransportProcess || m.Process == nil {
		return tools.Registration{}, fmt.Errorf("tool provider %s is not a process script provider", name)
	}
	if !knownPreparation(m.Preparation) {
		return tools.Registration{}, fmt.Errorf("tool provider %s names unknown preparation profile %q", name, m.Preparation)
	}
	// The registration keeps no storage the caller can change.
	m.Rule = m.Rule.Clone()
	m.Selectors.Shells = slices.Clone(m.Selectors.Shells)
	m.Selectors.Inputs = slices.Clone(m.Selectors.Inputs)
	p.Manifest = m
	if _, err := shellCatalogue(m.Rule, m.Selectors.Shells); err != nil {
		return tools.Registration{}, err
	}
	if err := analyzer.ValidateScriptInputs(m.Selectors.Inputs); err != nil {
		return tools.Registration{}, err
	}
	if _, generic := newPreparation(m, routeProcess).(genericPreparation); generic && len(m.Selectors.Shells)+len(m.Selectors.Inputs) == 0 {
		return tools.Registration{}, fmt.Errorf("tool provider %s selects no scripts; declare selectors.shells or selectors.inputs", name)
	}
	registration := tools.Registration{
		Descriptor:   m.Rule.Clone(),
		ScriptInputs: slices.Clone(m.Selectors.Inputs),
		// A run step that sets no shell runs in the runner's default shell,
		// which only execution resolution knows, so selectors.shells needs
		// it to match such steps. capabilities.resources needs each step's
		// working directory. See decision b4 in
		// docs/design/tool-providers-decisions.md.
		RequiresExecution: len(m.Selectors.Shells) != 0 || len(m.Capabilities.Resources) != 0,
		PrepareInput:      newPreparation(m, routeEngine).input,
		Enabled: func(c tools.Context) bool {
			possible, err := ProjectMayEnable(name, c.Config, c.ProjectConfig, c.ProjectRoot != "")
			return sel.selected(name) && (possible || err != nil)
		},
		Required: func(c tools.Context) bool {
			o := sel.forConfig(name, c.Config)
			options := ProcessOptions(name, o.Command, o.Process, c.Config, c.WorkingDir)
			return o.Backend != nil || Module(name, o.Command, o.Process, c.Config) != nil || options == nil && o.Runner == nil || options != nil && !options.Optional
		},
		NeedsProcess: func(_ context.Context, cfg, project *config.Config, inside bool) (bool, error) {
			return ProjectMayNeedProcessFor(name, cfg, project, inside, func(candidate *config.Config) bool {
				o := sel.forConfig(name, candidate)
				return o.Backend == nil && o.Runner == nil && processconfig.Enabled(o.Command, o.Process) && Module(name, o.Command, o.Process, candidate) == nil
			})
		},
		Validate: func() error {
			if sel.Backend != nil && (sel.Process != nil || sel.Command != "" || sel.Runner != nil) {
				return fmt.Errorf("%s selects both a backend and a process", name)
			}
			return nil
		},
		ConfiguredBackend: func(c tools.Context) (analyzer.ScriptBackend, error) {
			if sel.Backend != nil {
				return sel.Backend, nil
			}
			return Load(name, sel.Command, sel.Process, c.Config, engineLoader(c))
		},
	}
	registration.WithBackend = func(backend analyzer.ScriptBackend) (tools.Registration, error) {
		if !sel.selected(name) {
			return registration, nil
		}
		if !sel.defaultCommand(name) {
			return tools.Registration{}, fmt.Errorf("%s plugin conflicts with an explicitly selected backend or command", name)
		}
		next := p
		next.Selection = Selection{Backend: backend, ManagedOnly: sel.ManagedOnly}
		return register(next, selection{Selection: next.Selection, fallback: true})
	}
	probes := &versionProbes{}
	registration.Build = func(c tools.Context) (tools.Runner, error) {
		transports := &transports{provider: p, selection: sel, registration: registration, context: c, built: map[string]tools.Runner{}, probes: probes}
		if _, err := transports.forConfig(c.Config); err != nil {
			return nil, err
		}
		return transportRouting{transports: transports}, nil
	}
	if p.Bind != nil {
		registration = p.Bind(registration, p)
	}
	return registration, nil
}

// engineLoader loads repository-selected WebAssembly engines through the
// invocation's loader.
func engineLoader(c tools.Context) func(string) (analyzer.ScriptBackend, error) {
	if c.LoadEngine == nil {
		return nil
	}
	return func(path string) (analyzer.ScriptBackend, error) { return c.LoadEngine(c.Processes.Ctx, path) }
}

// transports builds the runner each configuration selects, sharing a
// runner between configurations that select the same transport.
type transports struct {
	provider     Provider
	selection    selection
	registration tools.Registration
	context      tools.Context
	built        map[string]tools.Runner
	selected     map[*config.Config]string
	probes       *versionProbes
}

// versionProbes keeps one version probe per process transport key for every
// runner a registration builds, so each command is probed, and a failed
// probe warned about, once per registration.
type versionProbes struct {
	mu    sync.Mutex
	byKey map[string]*process.VersionProbe
}

// get returns the probe of the transport with key, creating it when absent.
func (p *versionProbes) get(key string) *process.VersionProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byKey == nil {
		p.byKey = map[string]*process.VersionProbe{}
	}
	if p.byKey[key] == nil {
		p.byKey[key] = &process.VersionProbe{}
	}
	return p.byKey[key]
}

// forConfig returns the key of the runner cfg selects, building it once;
// "" selects none.
func (t *transports) forConfig(cfg *config.Config) (string, error) {
	if key, ok := t.selected[cfg]; ok {
		return key, nil
	}
	key, err := t.build(cfg)
	if err != nil {
		return "", err
	}
	if t.selected == nil {
		t.selected = map[*config.Config]string{}
	}
	t.selected[cfg] = key
	return key, nil
}

func (t *transports) build(cfg *config.Config) (string, error) {
	m := t.provider.Manifest
	name := m.Rule.Name
	o := t.selection.forConfig(name, cfg)
	c := t.context
	switch {
	case o.Backend != nil:
		return t.engine("backend", o.Backend)
	case o.Runner != nil:
		if _, ok := t.built["embedded"]; ok {
			return "embedded", nil
		}
		if c.Processes == nil {
			return "", fmt.Errorf("tool provider %s needs a process pool", name)
		}
		command := c.Processes.EmbeddedCommand(m.Process.Entry, o.Runner)
		if m.Process.UserArgs != nil {
			// An embedded runner sees no ambient environment.
			for _, variable := range m.Process.UserArgs.EnvOptions {
				command.Env = append(command.Env, variable+"=")
			}
		}
		return t.process("embedded", command, routeEmbedded, true)
	}
	if module := Module(name, o.Command, o.Process, cfg); module != nil {
		key := "wasm:" + module.Path
		if _, ok := t.built[key]; ok {
			return key, nil
		}
		engine, err := Load(name, o.Command, o.Process, cfg, engineLoader(c))
		if err != nil {
			return "", fmt.Errorf("could not initialize %s: %w", name, err)
		}
		return t.engine(key, engine)
	}
	if !processconfig.Enabled(o.Command, o.Process) {
		return "", nil
	}
	if err := RequireProcessOptIn(name, o.Command, o.Process, cfg, c.AllowConfigProcesses); err != nil {
		return "", err
	}
	options := ProcessOptions(name, o.Command, o.Process, cfg, c.WorkingDir)
	command := o.Command
	if command == name {
		command = m.Process.Entry
	}
	encoded, err := json.Marshal(struct {
		Command string
		Options *processconfig.Options
	}{command, options})
	if err != nil {
		return "", err
	}
	key := "process:" + string(encoded)
	if _, ok := t.built[key]; ok {
		return key, nil
	}
	if c.Processes == nil {
		return "", fmt.Errorf("tool provider %s needs a process pool", name)
	}
	resolved, err := c.Processes.ConfiguredCommandRunner(command, options, false)
	if err != nil {
		return "", fmt.Errorf("could not initialize %s: %w", name, err)
	}
	return t.process(key, resolved, routeProcess, options == nil || !options.Optional)
}

// process builds the runner of a process transport.
func (t *transports) process(key string, command *analyzerhost.Command, via route, required bool) (string, error) {
	m := t.provider.Manifest
	backend, err := process.NewBackend(m, command, process.Options{Programs: t.provider.Programs, Required: required, Probe: t.probes.get(key), Warn: t.context.Warn, Debug: t.context.Debug})
	if err != nil {
		return "", err
	}
	descriptor := analyzer.ScriptEngineDescriptor{Rule: m.Rule, Shells: m.Selectors.Shells, ScriptInputs: m.Selectors.Inputs, TemplateContract: m.Capabilities.TemplateContract}
	return t.register(key, descriptor, backend, via)
}

// engine builds the runner of a snippet engine, which declares its own
// template contract.
func (t *transports) engine(key string, engine analyzer.ScriptBackend) (string, error) {
	if _, ok := t.built[key]; ok {
		return key, nil
	}
	m := t.provider.Manifest
	descriptor := analyzer.ScriptEngineDescriptor{Rule: m.Rule, Shells: m.Selectors.Shells, TemplateContract: templateContract(engine)}
	return t.register(key, descriptor, engine, routeEngine)
}

func (t *transports) register(key string, descriptor analyzer.ScriptEngineDescriptor, engine analyzer.ScriptBackend, via route) (string, error) {
	m := t.provider.Manifest
	options := engineOptions{
		enabled:  t.registration.Enabled,
		batch:    newPreparation(m, via).batch,
		filename: m.Selectors.Filename,
		findings: findingCapabilities{fixes: m.Capabilities.Fixes, reportedSources: m.Capabilities.ReportedSources, messageTemplate: m.Capabilities.MessageTemplate, severities: m.Capabilities.Severities},
	}
	registration, err := registerEngine(descriptor, engine, options)
	if err != nil {
		return "", err
	}
	runner, err := registration.Build(t.context)
	if err != nil {
		return "", err
	}
	t.built[key] = runner
	return key, nil
}

// templateContract is the template-effects contract an engine declares: the
// result of its TemplateContract method, as embedding programs' backends
// declare it, or the contract in its script descriptor.
func templateContract(engine analyzer.ScriptBackend) string {
	if declared, ok := engine.(interface{ TemplateContract() string }); ok {
		return declared.TemplateContract()
	}
	if described, ok := engine.(analyzer.DescribedScriptBackend); ok {
		return described.ScriptDescriptor().TemplateContract
	}
	return ""
}

// transportRouting runs each script, and each scope, with the transport its
// configuration selects. A configuration whose optional transport cannot be
// built is skipped.
type transportRouting struct{ transports *transports }

func (r transportRouting) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	t := r.transports
	var failures error
	failed := map[*config.Config]bool{}
	keyOf := func(cfg *config.Config) string {
		if failed[cfg] {
			return ""
		}
		key, err := t.forConfig(cfg)
		if err != nil {
			failed[cfg] = true
			c := t.context
			c.Config = cfg
			if t.registration.Required(c) {
				failures = errors.Join(failures, err)
			}
			return ""
		}
		return key
	}
	var order []string
	groups := map[string]*tools.Batch{}
	indices := map[string][]int{}
	group := func(key string) *tools.Batch {
		if groups[key] == nil {
			order = append(order, key)
			groups[key] = &tools.Batch{CallGraph: batch.CallGraph}
		}
		return groups[key]
	}
	for _, scope := range batch.Scopes {
		var cfg *config.Config
		if scope != nil {
			cfg = scope.Config
		}
		if key := keyOf(cfg); key != "" {
			selected := group(key)
			selected.Scopes = append(selected.Scopes, scope)
		}
	}
	for i, script := range batch.Scripts {
		if key := keyOf(script.Config); key != "" {
			selected := group(key)
			selected.Scripts = append(selected.Scripts, script)
			indices[key] = append(indices[key], i)
		}
	}
	out := make([]tools.Finding, 0, len(batch.Scripts))
	for _, key := range order {
		findings, err := t.built[key].Analyze(ctx, *groups[key])
		out = append(out, restoreIndices(findings, indices[key])...)
		failures = errors.Join(failures, err)
	}
	return out, failures
}
