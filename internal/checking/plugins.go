package checking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// CheckWithPluginPaths loads plugins from paths, checks the input and closes the plugins.
func CheckWithPluginPaths(ctx context.Context, options Options, input inputmodel.Request, paths []string, acquisition plugininstall.Acquisition) (result *Result, err error) {
	sources, err := plugininstall.SourcesFromArguments(paths, acquisition)
	if err != nil {
		return nil, err
	}
	return CheckWithPlugins(ctx, options, input, sources)
}

// CheckWithPlugins loads the selected plugins and checks the input.
func CheckWithPlugins(ctx context.Context, options Options, input inputmodel.Request, sources plugininstall.Sources) (result *Result, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	if sources.Releases != nil {
		if err := sources.Releases.CheckStatuses(ctx, plugininstall.StatusOptions{Warning: func(message string) { _, _ = fmt.Fprintln(log, "warning:", message) }, Acquisition: sources.Acquisition}); err != nil {
			return nil, &pluginexec.LoadError{Err: err}
		}
	}
	plugins, err := pluginexec.Load(ctx, sources, BuiltinRuleNames(), log, options.Debug)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.Close(context.WithoutCancel(ctx))) }()
	options, err = ConfigurePlugins(plugins, ctx, options)
	if err != nil {
		return nil, err
	}
	options, err = ConfigureManagedPlugins(plugins, options)
	if err != nil {
		return nil, err
	}
	if options.LoadScriptEngine == nil {
		options.LoadScriptEngine = pluginexec.ScriptEngineLoader(sources.Acquisition, BuiltinRuleNames(), log, options.Debug)
	}
	return Check(ctx, options, input)
}

// RequiredToolsWithPluginPaths applies plugin replacements before deciding which native tools
// must be provisioned. It shares plugin validation and lifetime with Check.
func RequiredToolsWithPluginPaths(ctx context.Context, options Options, paths, plugins []string, acquisition plugininstall.Acquisition) (needed ToolRequirements, err error) {
	sources, err := plugininstall.SourcesFromArguments(plugins, acquisition)
	if err != nil {
		return needed, err
	}
	return RequiredToolsWithPlugins(ctx, options, paths, sources)
}

// RequiredToolsWithPlugins applies the selected plugins before checking which
// native tools are needed.
func RequiredToolsWithPlugins(ctx context.Context, options Options, paths []string, sources plugininstall.Sources) (needed ToolRequirements, err error) {
	loaded, err := pluginexec.Load(ctx, sources, BuiltinRuleNames(), io.Discard, false)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, loaded.Close(context.WithoutCancel(ctx))) }()
	options, err = ConfigurePlugins(loaded, ctx, options)
	if err != nil {
		return nil, err
	}
	options, err = ConfigureManagedPlugins(loaded, options)
	if err != nil {
		return nil, err
	}
	return RequiredTools(ctx, options, paths)
}

func ConfigureManagedPlugins(l *pluginexec.Loaded, options Options) (Options, error) {
	occupied := make([]string, 0, len(l.ScriptEngines))
	for _, engine := range l.ScriptEngines {
		occupied = append(occupied, engine.Descriptor.Engine)
	}
	configured, err := ConfigureToolIntegrations(options, occupied...)
	if err != nil {
		return options, &pluginexec.LoadError{Err: err}
	}
	return configured, nil
}

func ConfigurePlugins(l *pluginexec.Loaded, ctx context.Context, options Options) (Options, error) {
	if options.ConfigConnections.Transport == nil {
		options.ConfigConnections.Transport = l.Acquisition.Connections.Transport
	}
	if options.ConfigConnections.Resolver == nil {
		options.ConfigConnections.Resolver = l.Acquisition.Connections.Resolver
	}
	resolvers := l.ReferenceResolvers()
	if len(resolvers) > 1 || len(resolvers) != 0 && options.Resolver != nil {
		return options, &pluginexec.LoadError{Err: errors.New("select one reference resolver")}
	}
	if len(resolvers) == 0 && options.Resolver == nil {
		if provider, ok := options.RemoteActions.(interface{ ResolverEnabled() bool }); ok && provider.ResolverEnabled() {
			resolver, err := l.LoadDefaultResolver(ctx)
			if err != nil {
				return options, &pluginexec.LoadError{Err: err}
			}
			options.Resolver = resolver
		}
	} else if len(resolvers) == 1 {
		options.Resolver = resolvers[0]
	}
	if provider, ok := options.RemoteActions.(interface{ ResolverEnabled() bool }); ok && !provider.ResolverEnabled() {
		options.Resolver = nil
	}
	if options.Resolver != nil {
		if access, ok := options.RemoteActions.(repository.Access); ok {
			options.Resolver = repository.Bind(options.Resolver, access)
		}
	}
	seen := map[string]bool{}
	for _, engine := range l.ScriptEngines {
		metadata := engine.Descriptor
		if seen[metadata.Engine] {
			return options, &pluginexec.LoadError{Err: fmt.Errorf("multiple plugins provide script engine %q", metadata.Engine)}
		}
		seen[metadata.Engine] = true
		found := false
		options.Tools = slices.Clone(options.Tools)
		for i, registration := range options.Tools {
			if registration.Descriptor.Name != metadata.Engine {
				continue
			}
			if registration.WithBackend == nil {
				return options, &pluginexec.LoadError{Err: fmt.Errorf("tool %q cannot replace its backend", metadata.Engine)}
			}
			replacement, err := registration.WithBackend(engine.Backend)
			if err != nil {
				return options, &pluginexec.LoadError{Err: err}
			}
			replacement, err = scripts.WithScriptInputs(replacement, metadata, engine.Backend)
			if err != nil {
				return options, &pluginexec.LoadError{Err: err}
			}
			options.Tools[i] = replacement
			found = true
			break
		}
		if !found {
			registration, err := scripts.RegisterEngine(metadata, engine.Backend)
			if err != nil {
				return options, &pluginexec.LoadError{Err: err}
			}
			options.Tools = append(options.Tools, registration)
		}
	}

	for _, local := range l.Providers {
		name := local.Files.Manifest.Rule.Name
		if seen[name] {
			return options, &pluginexec.LoadError{Err: fmt.Errorf("multiple plugins provide script engine %q", name)}
		}
		seen[name] = true
		l.ToolIDs = append(l.ToolIDs, name)
		if local.Repository && !options.AllowConfigProcesses {
			return options, &pluginexec.LoadError{Err: fmt.Errorf("tool provider %s from %s runs a native executable; use --allow-config-processes for trusted repository configuration", name, local.Files.Source)}
		}
		if local.Files.Source != "" {
			// Only now, after the check above, download the executable.
			artifactOptions := l.Acquisition.ArtifactOptions()
			artifactOptions.Network = local.Network
			fetched, err := local.Files.Materialize(ctx, artifactOptions)
			if err != nil {
				return options, &pluginexec.LoadError{Err: fmt.Errorf("plugin %s: %w", local.Files.Source, err)}
			}
			local.Files = fetched
		}
		if local.Files.Manifest.Role == manifests.RoleDocument {
			if options.Processes == nil {
				options.Processes = hostprocess.NewProcessPool(ctx, runtime.NumCPU())
			}
			l.Processes = options.Processes
			commandOptions := &processconfig.Options{WorkingDir: options.WorkingDir}
			if local.Files.Executable != "" {
				commandOptions.Executable = &local.Files.Executable
			}
			command, err := options.Processes.ConfiguredCommandRunner(local.Files.Manifest.Process.Entry, commandOptions, false)
			if err != nil {
				return options, &pluginexec.LoadError{Err: err}
			}
			checker, err := process.NewDocumentAnalyzer(local.Files.Manifest, command, process.Options{Programs: local.Files.Programs, Required: true})
			if err != nil {
				return options, &pluginexec.LoadError{Err: err}
			}
			options.Analyzers = append(slices.Clone(options.Analyzers), checkinputs.Registration{Checker: checker})
			continue
		}
		selection := scripts.Selection{Command: name}
		if local.Files.Executable != "" {
			selection.Process = &processconfig.Options{Executable: &local.Files.Executable}
		}
		// An explicitly selected manifest replaces the tool's default route.
		registration, err := scripts.RegisterProvider(scripts.Provider{Manifest: local.Files.Manifest, Programs: local.Files.Programs, Selection: selection})
		if err != nil {
			return options, &pluginexec.LoadError{Err: err}
		}
		registration.ToolSettings = new(local.Files.Manifest.Settings())
		options.Tools = slices.DeleteFunc(slices.Clone(options.Tools), func(existing tools.Registration) bool { return existing.Descriptor.Name == name })
		options.Tools = append(options.Tools, registration)
	}

	options.Tools = slices.Clone(options.Tools)
	for i, registration := range options.Tools {
		options.Tools[i] = scripts.Configure(registration)
	}
	options.Analyzers = slices.Concat(options.Analyzers, l.Registrations())
	options.Runners.Providers = slices.Concat(options.Runners.Providers, l.RunnerProviders())
	return options, checkPluginTools(l, options.Tools)
}

// checkTools reports the tool ids named by --tool or --no-tool that match no
// tool registration, no loaded plugin and no skipped plugin.
func checkPluginTools(l *pluginexec.Loaded, registrations []tools.Registration) error {
	known := slices.Concat(l.SkippedTools, l.ToolIDs)
	for _, registration := range registrations {
		known = append(known, registration.Descriptor.Name)
	}
	var unknown []string
	for _, id := range l.NamedTools {
		if !slices.Contains(known, id) && !slices.Contains(unknown, id) {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return NewUnknownToolError(unknown, known)
}

// UnknownToolError reports tool ids that name no tool provider.
type UnknownToolError struct {
	// ids are the unknown ids in the order they were named.
	ids []string
	// known lists every id that names a provider, sorted and unique.
	known []string
}

// NewUnknownToolError reports the unknown tool ids ids, listing the ids in
// known as the valid choices.
func NewUnknownToolError(ids, known []string) *UnknownToolError {
	known = slices.Clone(known)
	slices.Sort(known)
	return &UnknownToolError{ids: slices.Clone(ids), known: slices.Compact(known)}
}

// IDs returns the unknown ids in the order they were named.
func (e *UnknownToolError) IDs() []string { return slices.Clone(e.ids) }

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("unknown tool %s; valid tools: %s", strings.Join(e.ids, ", "), strings.Join(e.known, ", "))
}
