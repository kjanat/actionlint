// Package checking coordinates input selection, rule execution and runtime ownership.
// It has no terminal, command parser, output destination or exit-code policy.
package checking

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptengines "actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// Options selects configuration and analysis backends for one application session.
type Options struct {
	// Processes shares the invocation's process budget with native analyzers.
	// Nil creates an application-owned pool.
	Processes     *process.ProcessPool
	RemoteActions actions.Provider
	// RemoteWorkflows fetches reusable workflows. If nil, RemoteActions supplies
	// them when it also implements workflows.Provider.
	RemoteWorkflows workflowinputs.Provider
	// Repository reads host knowledge once for each project root. Nil leaves facts unknown.
	Repository func(context.Context, string) repository.Snapshot
	Runners    runner.Options
	Tools      []tools.Registration
	Resolver   references.Resolver
	// SuggestCommitPins asks the resolver for commit-pin diagnostics as well as metadata.
	SuggestCommitPins bool
	WorkingDir        string
	// ConfigFile resolves relative to WorkingDir unless it is absolute or remote.
	ConfigFile        string
	ConfigConnections acquisitionhttp.Connections
	ConfigOverlays    []repoconfig.Overlay
	SkipProjectConfig bool
	// NoIgnore disables Git ignore rules and configured file filters.
	NoIgnore  bool
	Analyzers []checkinputs.Registration
	ReadFile  func(string) ([]byte, error)
	// SourceOverlays supplies all editor buffers, independently of selected
	// entries. They become explicit YAML captures before analysis begins.
	SourceOverlays               []SourceOverlay
	OnConfigLoaded               func(configreport.Report)
	OnFilesSelected              func([]string)
	LoadScriptEngine             func(context.Context, string) (checkinputs.ScriptBackend, error)
	Presets                      repoconfig.Presets
	IgnorePatterns               []string
	IgnoreSelectors              policy.IgnoreSelectors
	LogWriter                    io.Writer
	Verbose, Debug, LogSelection bool
	LegacyErrors                 bool
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
}

// SourceOverlay supplies bytes without selecting the document as an entry.
type SourceOverlay struct {
	Path    string
	Content []byte
}

// Result is independent of the engine's traversal and mutable session state.
// Rendering and application-specific exit policies consume the same findings.
type Result = report.Analysis

// Check manages the analysis runtime and returns findings collected before any error.
func Check(ctx context.Context, options Options, input inputmodel.Request) (result *Result, err error) {
	if err := validateAnalyzerRegistrations(options.Analyzers); err != nil {
		return nil, err
	}
	var definitions []commands.Definition
	for _, registration := range options.Analyzers {
		if !validation.IsNil(registration.Checker) {
			definitions = append(definitions, registration.Checker.Descriptor().InvocationDefinitions...)
		}
	}
	for _, registration := range options.Tools {
		definitions = append(definitions, registration.InvocationDefinitions...)
	}
	registry, err := interpreters.New(definitions)
	if err != nil {
		return nil, err
	}
	ctx = commands.WithRegistry(ctx, registry)
	ctx, _ = accounting.TrackRequests(ctx)
	options, err = ConfigureToolIntegrations(options)
	if err != nil {
		return nil, err
	}
	if options.Resolver != nil {
		if factory, ok := options.Resolver.(references.SessionFactory); ok {
			options.Resolver = factory.NewSession()
		}
		if provider, ok := options.RemoteActions.(interface {
			WithResolver(references.Resolver) actions.Provider
		}); ok {
			options.RemoteActions = provider.WithResolver(options.Resolver)
		}
	}
	if options.Resolver != nil && options.SuggestCommitPins {
		resolutions := references.NewResolutionSession(options.Resolver)
		options.Analyzers = append(slices.Clone(options.Analyzers), checkinputs.Registration{
			Checker: security.NewResolvedReferenceCheck(),
			Prepare: func(ctx context.Context, input checkinputs.Input) (checkinputs.Input, error) {
				resolved, err := resolutions.ResolveDependencies(ctx, input.Dependencies)
				input.ResolvedDependencies = resolved
				return input, err
			},
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selectionReader := options.ReadFile
	overlays := make([]SourceOverlay, len(options.SourceOverlays))
	if len(overlays) != 0 {
		base, err := filepath.Abs(options.WorkingDir)
		if err != nil {
			return nil, err
		}
		byAlias := make(map[string]int, len(overlays))
		for index, overlay := range options.SourceOverlays {
			path := overlay.Path
			if path != "<stdin>" && !filepath.IsAbs(path) {
				path = filepath.Join(base, path)
			}
			if path != "<stdin>" {
				path = filepath.Clean(path)
			}
			overlays[index] = SourceOverlay{Path: path, Content: bytes.Clone(overlay.Content)}
			// For duplicate path spellings, discovery reads the lexicographically
			// smallest content. Capture preflight validates every variant before analysis.
			previous, exists := byAlias[path]
			if !exists || bytes.Compare(overlays[index].Content, overlays[previous].Content) < 0 {
				byAlias[path] = index
			}
		}
		backing := configsource.New(ctx, options.ReadFile, options.ConfigConnections)
		selectionReader = func(path string) ([]byte, error) {
			if address.Remote(path) {
				return backing.ReadFile(path)
			}
			alias := path
			if path != "<stdin>" {
				var err error
				alias, err = filepath.Abs(path)
				if err != nil {
					return nil, err
				}
			}
			if index, exists := byAlias[alias]; exists {
				return bytes.Clone(overlays[index].Content), nil
			}
			return backing.ReadFile(path)
		}
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}

	processes := options.Processes
	if processes == nil {
		processes = process.NewProcessPool(ctx, runtime.NumCPU())
	}
	engines := scriptengines.NewScriptEngines(options.LoadScriptEngine)
	defer func() { processes.Wait(); err = errors.Join(err, engines.Close(context.WithoutCancel(ctx))) }()
	request := analysisRequest{Processes: processes, Runners: options.Runners, Analyzers: options.Analyzers, ReadFile: options.ReadFile, Tools: options.Tools, RulePresets: options.Presets, LogWriter: log, AllowConfigProcesses: options.AllowConfigProcesses}
	request.SourceOverlays = overlays
	request.NoIgnore = options.NoIgnore
	request.RemoteActions = options.RemoteActions
	request.RemoteWorkflows = options.RemoteWorkflows
	if request.RemoteWorkflows == nil {
		request.RemoteWorkflows, _ = options.RemoteActions.(workflowinputs.Provider)
	}
	request.Repository = options.Repository
	request.DiskSources = input.Kind == inputmodel.Files || input.Kind == inputmodel.Repository

	if options.LoadScriptEngine != nil {
		request.LoadScriptEngine = engines.Load
	}
	if options.Verbose {
		request.LogLevel = LogLevelVerbose
	} else if options.Debug {
		request.LogLevel = LogLevelDebug
	}
	request.IgnorePatterns, err = repoconfig.CompileIgnorePatterns(options.IgnorePatterns)
	request.IgnoreSelectors = options.IgnoreSelectors
	if err != nil {
		return nil, err
	}
	if input.Kind == inputmodel.Prepared {
		request.Sources, request.WorkingDir = slices.Clone(input.Sources), options.WorkingDir
		policy, err := repoconfig.LoadNetworkPolicy(ctx, options.ConfigConnections)
		if err != nil {
			return nil, err
		}
		for i := range request.Sources {
			request.Sources[i].Config, err = policy.Apply(request.Sources[i].Config)
			if err != nil {
				return nil, err
			}
		}
		if options.WorkingDir != "" {
			request.LocalDependencyRoot, err = filepath.Abs(options.WorkingDir)
			if err != nil {
				return nil, err
			}
		}
		result, err := analyzePrepared(ctx, request)
		if result != nil {
			result.Inputs = append(result.Inputs, policy.Files()...)
		}
		return result, err
	}
	var names []string
	for _, registration := range options.Analyzers {
		if !validation.IsNil(registration.Checker) {
			names = append(names, registration.Checker.Descriptor().Name)
		}
	}
	for _, registration := range options.Tools {
		names = append(names, registration.Descriptor.Name)
	}
	selectionLog := func(args ...any) {
		if options.LogSelection && request.LogLevel >= LogLevelVerbose {
			_, _ = io.WriteString(log, "verbose: "+fmt.Sprintln(args...))
		}
	}
	rules, err := ConfigurationRules().WithRegisteredRules(configurationRegistrations(options.Analyzers, options.Tools)...)
	if err != nil {
		return nil, err
	}
	session, err := workspace.New(ctx, workspace.Options{
		Rules:      rules,
		WorkingDir: options.WorkingDir, StdinFileName: input.Name,
		ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, OnConfigLoaded: options.OnConfigLoaded, OnFilesSelected: options.OnFilesSelected, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile: selectionReader, ConfigConnections: options.ConfigConnections, RuleNames: names, Log: selectionLog, LogWriter: log,
		NoIgnore: options.NoIgnore,
	})
	if err != nil {
		return nil, err
	}
	var selection *workspace.Selection
	switch input.Kind {
	case inputmodel.Files:
		if input.Selection != nil {
			selection, err = session.SelectedFiles(*input.Selection, nil)
		} else {
			selection, err = session.Files(input.Paths, nil)
		}
	case inputmodel.Stdin:
		if input.Reader == nil {
			return nil, errors.New("stdin source requires a reader")
		}
		selection, err = session.ReadStdin(input.Reader, input.NormalizePath)
	case inputmodel.Repository:
		if input.Selection != nil && len(input.Selection.Files) != 0 {
			selection, err = session.SelectedFiles(*input.Selection, nil)
			break
		}
		var listed []string
		empty := false
		if input.AllowEmptyRepository {
			empty, listed, err = emptyRepository(ctx, options.WorkingDir)
			if err != nil {
				return nil, err
			}
		}
		if empty {
			if options.Verbose {
				_, _ = fmt.Fprintf(log, "verbose: No workflow files to check from %q\n", options.WorkingDir)
			}
			selection, err = session.Empty()
		} else {
			selection, err = session.Repository(input.Directory)
		}
		if selection != nil {
			selection.Inputs = slices.Compact(slices.Sorted(slices.Values(append(selection.Inputs, listed...))))
		}
	case inputmodel.Prepared:
		// Prepared sources were analyzed above and never reach selection.
		fallthrough
	default:
		return nil, fmt.Errorf("unknown input kind %d", input.Kind)
	}
	if err == nil {
		request.Sources = selection.Sources
		request.WorkingDir = session.WorkingDir()
		result, err = analyzePrepared(ctx, request)
		if result != nil {
			result.Configurations = selection.Configurations
			result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, selection.Inputs...))))
		}
	}
	if options.LegacyErrors && err != nil {
		err = LegacyAnalysisError(err)
	}
	return result, err
}

func emptyRepository(ctx context.Context, directory string) (bool, []string, error) {
	var listed []string
	reader := files.New(ctx, nil, func(path string) { listed = append(listed, path) })
	hasWorkflows := false
	var err error
	if root := discovery.RepositoryRoot(directory); root != "" {
		hasWorkflows, err = discovery.HasWorkflowFiles(root, reader)
		if err != nil {
			return false, nil, err
		}
	}
	typo, _ := discovery.WorkflowTypo(directory, reader)
	return typo == "" && !hasWorkflows, listed, nil
}

// Analyze checks supplied snapshots through the same runtime ownership path as Check.
// It does not discover configuration or read the supplied source paths.
func Analyze(ctx context.Context, options Options, sources []workspace.Source) (*Result, error) {
	ctx, _ = accounting.TrackRequests(ctx)
	return Check(ctx, options, inputmodel.Request{Kind: inputmodel.Prepared, Sources: sources})
}

func analyzePrepared(ctx context.Context, request analysisRequest) (*Result, error) {
	analyzed, err := runAnalysis(ctx, request)
	if analyzed == nil {
		return nil, err
	}
	return &analyzed.Analysis, err
}
