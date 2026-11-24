// Package assembly loads the plugins selected by an invocation through
// internal/plugins and registers their analyzers, script engines, tool
// providers, runners and reference resolvers in the checking options.
package assembly

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/backends"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/externallinters/process"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func CheckWithPluginPaths(ctx context.Context, options checking.Options, input checkrequest.Request, paths []string, acquisition dependencycache.Acquisition) (result *report.Analysis, err error) {
	sources, err := pluginselection.SourcesFromArguments(paths, acquisition)
	if err != nil {
		return nil, err
	}
	return CheckWithPlugins(ctx, options, input, sources)
}

func CheckWithPlugins(ctx context.Context, options checking.Options, input checkrequest.Request, sources pluginselection.Sources) (result *report.Analysis, err error) {
	session, err := Open(ctx, options, sources)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close(ctx)) }()
	options = session.Options
	if options.LoadScriptEngine == nil {
		log := options.LogWriter
		if log == nil {
			log = io.Discard
		}
		options.LoadScriptEngine = pluginexecution.ScriptEngineLoader(sources.Acquisition, checking.BuiltinRuleNames(), log, options.Debug)
	}
	result, err = checking.Check(ctx, options, input)
	if result != nil {
		result.Providers = providerStatuses(sources, options.Tools, result)
	}
	return result, err
}

func RequiredToolsWithPluginPaths(ctx context.Context, options checking.Options, paths, plugins []string, acquisition dependencycache.Acquisition) (needed workspace.ExternalToolRequirements, err error) {
	sources, err := pluginselection.SourcesFromArguments(plugins, acquisition)
	if err != nil {
		return needed, err
	}
	return RequiredToolsWithPlugins(ctx, options, paths, sources)
}

func RequiredToolsWithPlugins(ctx context.Context, options checking.Options, paths []string, sources pluginselection.Sources) (needed workspace.ExternalToolRequirements, err error) {
	options.LogWriter, options.Debug = io.Discard, false
	session, err := Open(ctx, options, sources)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close(ctx)) }()
	return checking.RequiredTools(ctx, session.Options, paths)
}

func ConfigureManagedPlugins(l *pluginexecution.Loaded, options checking.Options) (checking.Options, error) {
	occupied := make([]string, 0, len(l.ScriptEngines))
	for _, engine := range l.ScriptEngines {
		occupied = append(occupied, engine.Descriptor.Engine)
	}
	configured, err := checking.ConfigureToolIntegrations(options, occupied...)
	if err != nil {
		return options, &pluginexecution.LoadError{Err: err}
	}
	return configured, nil
}

func ConfigurePlugins(l *pluginexecution.Loaded, ctx context.Context, options checking.Options) (checking.Options, error) {
	if options.ConfigConnections.Transport == nil {
		options.ConfigConnections.Transport = l.Acquisition.Connections.Transport
	}
	if options.ConfigConnections.Resolver == nil {
		options.ConfigConnections.Resolver = l.Acquisition.Connections.Resolver
	}
	resolvers := l.ReferenceResolvers()
	if len(resolvers) > 1 || len(resolvers) != 0 && options.Resolver != nil {
		return options, &pluginexecution.LoadError{Err: errors.New("select one reference resolver")}
	}
	if len(resolvers) == 0 && options.Resolver == nil {
		if provider, ok := options.RemoteActions.(interface{ ResolverEnabled() bool }); ok && provider.ResolverEnabled() {
			resolver, err := l.LoadDefaultResolver(ctx)
			if err != nil {
				return options, &pluginexecution.LoadError{Err: err}
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
		if access, ok := options.RemoteActions.(references.RepositoryAccess); ok {
			options.Resolver = references.BindRepositoryAccess(options.Resolver, access)
		}
	}
	seen := map[string]bool{}
	for _, engine := range l.ScriptEngines {
		metadata := engine.Descriptor
		if seen[metadata.Engine] {
			return options, &pluginexecution.LoadError{Err: fmt.Errorf("multiple plugins provide script engine %q", metadata.Engine)}
		}
		seen[metadata.Engine] = true
		found := false
		options.Tools = slices.Clone(options.Tools)
		for i, registration := range options.Tools {
			if registration.Name != metadata.Engine {
				continue
			}
			if registration.WithBackend == nil {
				return options, &pluginexecution.LoadError{Err: fmt.Errorf("tool %q cannot replace its backend", metadata.Engine)}
			}
			replacement, err := registration.WithBackend(engine.Backend)
			if err != nil {
				return options, &pluginexecution.LoadError{Err: err}
			}
			replacement, err = backends.WithScriptInputs(replacement, metadata, engine.Backend)
			if err != nil {
				return options, &pluginexecution.LoadError{Err: err}
			}
			options.Tools[i] = replacement
			found = true
			break
		}
		if !found {
			registration, err := backends.RegisterEngine(metadata, engine.Backend)
			if err != nil {
				return options, &pluginexecution.LoadError{Err: err}
			}
			options.Tools = append(options.Tools, registration)
		}
	}

	for _, local := range l.Providers {
		name := local.Files.Manifest.Rule.Name
		if seen[name] {
			return options, &pluginexecution.LoadError{Err: fmt.Errorf("multiple plugins provide script engine %q", name)}
		}
		seen[name] = true
		l.ToolIDs = append(l.ToolIDs, name)
		if local.Repository && !options.AllowConfigProcesses {
			return options, &pluginexecution.LoadError{Err: fmt.Errorf("tool provider %s from %s runs a native executable; use --allow-config-processes for trusted repository configuration", name, local.Files.Source)}
		}
		if local.Files.Source != "" {
			// Download the executable only after the trust check above.
			artifactOptions := pluginacquisition.ArtifactOptions(l.Acquisition)
			artifactOptions.Network = local.Network
			fetched, err := local.Files.Materialize(ctx, artifactOptions)
			if err != nil {
				return options, &pluginexecution.LoadError{Err: fmt.Errorf("plugin %s: %w", local.Files.Source, err)}
			}
			local.Files = fetched
		}
		if local.Files.Manifest.Role == toolmanifest.RoleDocument {
			if options.Processes == nil {
				options.Processes = hostprocess.NewProcessPool(ctx, runtime.NumCPU())
			}
			l.Processes = options.Processes
			commandOptions := &hostprocess.CommandOptions{WorkingDir: options.WorkingDir}
			if local.Files.Executable != "" {
				commandOptions.Executable = &local.Files.Executable
			}
			command, err := options.Processes.ConfiguredCommandRunner(local.Files.Manifest.Process.Entry, commandOptions, false)
			if err != nil {
				return options, &pluginexecution.LoadError{Err: err}
			}
			analyzer, err := process.NewDocumentAnalyzer(local.Files.Manifest, command, process.Options{Programs: local.Files.Programs, Required: true})
			if err != nil {
				return options, &pluginexecution.LoadError{Err: err}
			}
			options.Analyzers = append(slices.Clone(options.Analyzers), results.AnalyzerRegistration{Analyzer: analyzer})
			continue
		}
		selection := backends.ToolSelection{Command: name}
		if local.Files.Executable != "" {
			selection.Process = &hostprocess.CommandOptions{Executable: &local.Files.Executable}
		}
		// An explicitly selected manifest replaces the tool's default route.
		registration, err := backends.RegisterProvider(backends.Provider{Manifest: local.Files.Manifest, Programs: local.Files.Programs, Selection: selection})
		if err != nil {
			return options, &pluginexecution.LoadError{Err: err}
		}
		registration.ToolSettings = new(local.Files.Manifest.Settings())
		options.Tools = slices.DeleteFunc(slices.Clone(options.Tools), func(existing contract.Registration) bool { return existing.Name == name })
		options.Tools = append(options.Tools, registration)
	}

	options.Tools = slices.Clone(options.Tools)
	for i, registration := range options.Tools {
		options.Tools[i] = backends.Configure(registration)
	}
	options.Analyzers = slices.Concat(options.Analyzers, l.Registrations())
	options.Runners.Providers = slices.Concat(options.Runners.Providers, l.RunnerProviders())
	return options, checkPluginTools(l, options.Tools)
}

func checkPluginTools(l *pluginexecution.Loaded, registrations []contract.Registration) error {
	known := slices.Concat(l.SkippedTools, l.ToolIDs)
	for _, registration := range registrations {
		known = append(known, registration.Name)
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

type UnknownToolError struct {
	ids   []string
	known []string
}

func NewUnknownToolError(ids, known []string) *UnknownToolError {
	known = slices.Clone(known)
	slices.Sort(known)
	return &UnknownToolError{ids: slices.Clone(ids), known: slices.Compact(known)}
}

func (e *UnknownToolError) IDs() []string { return slices.Clone(e.ids) }

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("unknown tool %s; valid tools: %s", strings.Join(e.ids, ", "), strings.Join(e.known, ", "))
}
