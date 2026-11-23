package backends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/externallinters/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/externallinters/process"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// ToolSelection disables the provider when it is the zero value.
type ToolSelection struct {
	// An empty Command disables the process unless Process names an executable.
	Command string
	// Process.Optional skips the selection with a warning when it cannot run.
	Process *hostprocess.CommandOptions
	Backend scriptengine.Backend
	// Runner has no filesystem access.
	Runner      func(args []string, stdin string) ([]byte, error)
	ManagedOnly bool
}

type Provider struct {
	Manifest  toolmanifest.Manifest
	Programs  toolmanifest.Programs
	Selection ToolSelection
	// Bind may be nil, and RegisterProvider applies it to every registration it derives.
	Bind func(contract.Registration, Provider) contract.Registration
}

type selection struct {
	ToolSelection
	fallback bool
}

func (s selection) defaultCommand(name string) bool {
	if s.Backend != nil || s.Runner != nil || s.Command != name {
		return false
	}
	if s.Process == nil {
		return true
	}
	options := *s.Process
	options.WorkingDir = ""
	return reflect.DeepEqual(options, hostprocess.CommandOptions{Optional: true})
}

func (s selection) forConfig(name string, cfg *configuration.Config) ToolSelection {
	if s.fallback && cfg != nil && cfg.Tools[name].Backend != nil {
		s.Backend, s.Command, s.Process = nil, name, &hostprocess.CommandOptions{Optional: true}
	}
	return s.ToolSelection
}

func (s selection) selected(name string) bool {
	return s.Backend != nil || s.Runner != nil || AllowsModule(name, s.Command, s.Process) || hostprocess.CommandEnabled(s.Command, s.Process)
}

func RegisterProvider(p Provider) (contract.Registration, error) {
	return register(p, selection{ToolSelection: p.Selection})
}

func register(p Provider, sel selection) (contract.Registration, error) {
	m := p.Manifest
	name := m.Rule.Name
	if m.Role != toolmanifest.RoleScript || m.Transport != toolmanifest.TransportProcess || m.Process == nil {
		return contract.Registration{}, fmt.Errorf("tool provider %s is not a process script provider", name)
	}
	if !knownPreparation(m.Preparation) {
		return contract.Registration{}, fmt.Errorf("tool provider %s names unknown preparation profile %q", name, m.Preparation)
	}
	if _, err := integrationconfig.New(m, p.Programs); err != nil {
		return contract.Registration{}, fmt.Errorf("tool provider %s configuration: %w", name, err)
	}
	if _, err := preparation.NewSourcePlanner(m.PreparationTransform, p.Programs); err != nil {
		return contract.Registration{}, fmt.Errorf("tool provider %s preparation: %w", name, err)
	}
	if _, err := shellCatalogue(m.Rule, m.Selectors.Shells); err != nil {
		return contract.Registration{}, err
	}
	if err := checkinputs.ValidateScriptInputs(m.Selectors.Inputs); err != nil {
		return contract.Registration{}, err
	}
	if _, generic := newPreparation(m, routeProcess, p.Programs).(genericPreparation); generic && len(m.Selectors.Shells)+len(m.Selectors.Inputs) == 0 {
		return contract.Registration{}, fmt.Errorf("tool provider %s selects no scripts; declare selectors.shells or selectors.inputs", name)
	}
	scope, err := m.Scope.ConfigurationScope()
	if err != nil {
		return contract.Registration{}, fmt.Errorf("tool provider %s: %w", name, err)
	}
	registration := contract.Registration{
		InvocationDefinitions: m.InvocationDefinitions,
		Descriptor:            m.Rule.Clone(),
		Scope:                 scope,
		SupportsActions:       true,
		ScriptInputs:          slices.Clone(m.Selectors.Inputs),
		RequiresExecution:     len(m.Selectors.Shells) != 0 || len(m.Capabilities.Resources) != 0,
		PrepareInput:          newPreparation(m, routeEngine, p.Programs).input,
		Enabled: func(c contract.Context) bool {
			possible, err := ProjectMayEnable(name, c.Config, c.ProjectConfig)
			return sel.selected(name) && (possible || err != nil)
		},
		Required: func(c contract.Context) bool {
			o := sel.forConfig(name, c.Config)
			options := ProcessOptions(name, o.Command, o.Process, c.Config, c.WorkingDir)
			return o.Backend != nil || Module(name, o.Command, o.Process, c.Config) != nil || options == nil && o.Runner == nil || options != nil && !options.Optional
		},
		NeedsProcess: func(_ context.Context, cfg, project *configuration.Config) (bool, error) {
			return ProjectMayNeedProcessFor(name, cfg, project, func(candidate *configuration.Config) bool {
				o := sel.forConfig(name, candidate)
				return o.Backend == nil && o.Runner == nil && hostprocess.CommandEnabled(o.Command, o.Process) && Module(name, o.Command, o.Process, candidate) == nil
			})
		},
		Validate: func() error {
			if sel.Backend != nil && (sel.Process != nil || sel.Command != "" || sel.Runner != nil) {
				return fmt.Errorf("%s selects both a backend and a process", name)
			}
			return nil
		},
		ConfiguredBackend: func(c contract.Context) (scriptengine.Backend, error) {
			if sel.Backend != nil {
				return sel.Backend, nil
			}
			return LoadConfiguredModule(name, sel.Command, sel.Process, c.Config, engineLoader(c))
		},
	}
	registration.WithBackend = func(backend scriptengine.Backend) (contract.Registration, error) {
		if !sel.selected(name) {
			return registration, nil
		}
		if !sel.defaultCommand(name) {
			return contract.Registration{}, fmt.Errorf("%s plugin conflicts with an explicitly selected backend or command", name)
		}
		next := p
		next.Selection = ToolSelection{Backend: backend, ManagedOnly: sel.ManagedOnly}
		return register(next, selection{ToolSelection: next.Selection, fallback: true})
	}
	probes := &versionProbes{}
	registration.Build = func(c contract.Context) (contract.Runner, error) {
		transports := &transports{provider: p, selection: sel, registration: registration, context: c, built: map[string]contract.Runner{}, probes: probes}
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

func engineLoader(c contract.Context) func(string) (scriptengine.Backend, error) {
	if c.LoadEngine == nil {
		return nil
	}
	return func(path string) (scriptengine.Backend, error) { return c.LoadEngine(c.Processes.Ctx, path) }
}

type transports struct {
	provider     Provider
	selection    selection
	registration contract.Registration
	context      contract.Context
	built        map[string]contract.Runner
	selected     map[*configuration.Config]string
	probes       *versionProbes
}

type versionProbes struct {
	mu    sync.Mutex
	byKey map[string]*process.VersionProbe
}

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

func (t *transports) forConfig(cfg *configuration.Config) (string, error) {
	if key, ok := t.selected[cfg]; ok {
		return key, nil
	}
	key, err := t.build(cfg)
	if err != nil {
		return "", err
	}
	if t.selected == nil {
		t.selected = map[*configuration.Config]string{}
	}
	t.selected[cfg] = key
	return key, nil
}

func (t *transports) build(cfg *configuration.Config) (string, error) {
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
		key := "wasm:" + module.Path()
		if _, ok := t.built[key]; ok {
			return key, nil
		}
		engine, err := LoadConfiguredModule(name, o.Command, o.Process, cfg, engineLoader(c))
		if err != nil {
			return "", fmt.Errorf("could not initialize %s: %w", name, err)
		}
		return t.engine(key, engine)
	}
	if !hostprocess.CommandEnabled(o.Command, o.Process) {
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
		Options *hostprocess.CommandOptions
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

func (t *transports) process(key string, command *hostprocess.Command, via route, required bool) (string, error) {
	m := t.provider.Manifest
	backend, err := process.NewBackend(m, command, process.Options{Programs: t.provider.Programs, Required: required, Probe: t.probes.get(key), Warn: t.context.Warn, Debug: t.context.Debug, Skipped: func() {
		if t.context.Skipped != nil {
			t.context.Skipped(m.Rule.Name)
		}
	}})
	if err != nil {
		return "", err
	}
	descriptor := scriptengine.Descriptor{Rule: m.Rule, Shells: m.Selectors.Shells, ScriptInputs: m.Selectors.Inputs, TemplateContract: m.Capabilities.TemplateContract}
	return t.register(key, descriptor, backend, via)
}

func (t *transports) engine(key string, engine scriptengine.Backend) (string, error) {
	if _, ok := t.built[key]; ok {
		return key, nil
	}
	m := t.provider.Manifest
	descriptor := scriptengine.Descriptor{Rule: m.Rule, Shells: m.Selectors.Shells, TemplateContract: templateContract(engine)}
	if described, ok := engine.(scriptengine.DescribedBackend); ok {
		descriptor.Resources = described.ScriptDescriptor().Resources
	}
	return t.register(key, descriptor, engine, routeEngine)
}

func (t *transports) register(key string, descriptor scriptengine.Descriptor, engine scriptengine.Backend, via route) (string, error) {
	m := t.provider.Manifest
	options := engineOptions{
		enabled:  t.registration.Enabled,
		batch:    newPreparation(m, via, t.provider.Programs).batch,
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

func templateContract(engine scriptengine.Backend) string {
	if declared, ok := engine.(interface{ TemplateContract() string }); ok {
		return declared.TemplateContract()
	}
	if described, ok := engine.(scriptengine.DescribedBackend); ok {
		return described.ScriptDescriptor().TemplateContract
	}
	return ""
}

type transportRouting struct{ transports *transports }

func (r transportRouting) Analyze(ctx context.Context, batch scriptanalysis.Batch) ([]contract.ToolFinding, error) {
	t := r.transports
	var failures error
	failed := map[*configuration.Config]bool{}
	keyOf := func(cfg *configuration.Config) string {
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
	groups := map[string]*scriptanalysis.Batch{}
	indices := map[string][]int{}
	group := func(key string) *scriptanalysis.Batch {
		if groups[key] == nil {
			order = append(order, key)
			groups[key] = &scriptanalysis.Batch{CallGraph: batch.CallGraph}
		}
		return groups[key]
	}
	for _, scope := range batch.Scopes {
		var cfg *configuration.Config
		if scope != nil {
			cfg = scope.Config
		}
		if key := keyOf(cfg); key != "" {
			selected := group(key)
			selected.Scopes = append(selected.Scopes, scope)
		}
	}
	for i, script := range batch.Scripts {
		if key := keyOf(script.Config()); key != "" {
			selected := group(key)
			selected.Scripts = append(selected.Scripts, script)
			indices[key] = append(indices[key], i)
		}
	}
	out := make([]contract.ToolFinding, 0, len(batch.Scripts))
	for _, key := range order {
		findings, err := t.built[key].Analyze(ctx, *groups[key])
		out = append(out, restoreIndices(findings, indices[key])...)
		failures = errors.Join(failures, err)
	}
	return out, failures
}
