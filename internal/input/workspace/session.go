// Package workspace prepares repository inputs and configuration without running analyzers.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/input/files"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

// Source associates the displayed path with the file read for analysis.
type Source struct {
	Path             string
	Content          []byte
	Config           *repoconfig.Config
	ConfigProvenance repoconfig.Provenance
	Project          *Project
	InputPath        string
	// ExplicitFile marks a literally named file.
	ExplicitFile bool
}

// Selection is a prepared batch. Inputs includes configuration and excluded discovery candidates.
type Selection struct {
	Sources        []Source
	Inputs         []string
	Configurations []configreport.Report
}

// Options controls only input preparation, not analysis or rendering.
type Options struct {
	// Rules supplies the declarations used to validate loaded configuration.
	Rules                                 repoconfig.RuleSet
	WorkingDir, StdinFileName, ConfigFile string
	// Config, when set, is used for every input instead of the repository's
	// configuration file. It cannot be combined with ConfigFile.
	Config            *repoconfig.Config
	ConfigConnections acquisitionhttp.Connections
	ConfigProvenance  repoconfig.Provenance
	ConfigOverlays    []repoconfig.Overlay
	OnConfigLoaded    func(configreport.Report)
	SkipProjectConfig bool
	NoIgnore          bool
	OnFilesSelected   func([]string)
	ReadFile          func(string) ([]byte, error)
	RuleNames         []string
	Log               func(...any)
	LogWriter         io.Writer
}

// Session shares repository and configuration lookups across input selections.
type Session struct {
	rules    repoconfig.RuleSet
	projects *Projects
	// defaultConfig comes from Options.Config or Options.ConfigFile and
	// replaces every project's configuration.
	defaultConfig           *repoconfig.Config
	defaultConfigProvenance repoconfig.Provenance
	defaultConfigPath       string
	configState             *configState
	ctx                     context.Context
	cwd, stdin              string
	onFilesSelected         func([]string)
	readFile                func(string) ([]byte, error)
	log                     func(...any)
	logOut                  io.Writer
	ruleNames               []string
	noIgnore                bool
}

// New prepares a session whose file reads and config loads stop when ctx is canceled.
func New(ctx context.Context, opts Options) (*Session, error) {
	if opts.Config != nil && opts.ConfigFile != "" {
		return nil, errors.New("workspace options set both Config and ConfigFile")
	}
	a := &Session{rules: opts.Rules, projects: NewProjects(opts.Rules), defaultConfig: opts.Config, ctx: ctx, cwd: opts.WorkingDir, stdin: opts.StdinFileName, onFilesSelected: opts.OnFilesSelected, readFile: configsource.New(ctx, opts.ReadFile, opts.ConfigConnections).ReadFile, log: opts.Log, logOut: opts.LogWriter, ruleNames: slices.Clone(opts.RuleNames)}
	a.defaultConfigProvenance = opts.ConfigProvenance
	a.noIgnore = opts.NoIgnore
	if a.log == nil {
		a.log = func(...any) {}
	}
	if a.logOut == nil {
		a.logOut = io.Discard
	}
	a.projects.ctx = ctx
	a.projects.readFile = a.readFile
	a.projects.ruleNames = a.ruleNames
	a.projects.skipConfig = opts.SkipProjectConfig
	a.configState = &configState{overlays: slices.Clone(opts.ConfigOverlays), onLoaded: opts.OnConfigLoaded, loaded: make(map[*Project]*repoconfig.Config), reports: make(map[*Project]configreport.Report)}
	var err error
	if a.cwd == "" {
		a.cwd, err = os.Getwd()
		if err != nil {
			a.cwd = "."
		}
	}
	a.cwd, err = filepath.Abs(a.cwd)
	if err != nil {
		return nil, err
	}
	if opts.ConfigFile != "" {
		path := opts.ConfigFile
		if !address.Remote(path) && !filepath.IsAbs(path) {
			path = filepath.Join(a.cwd, path)
		}
		source, err := a.rules.ReadContext(ctx, path, a.readFile, a.ruleNames...)
		if err != nil {
			return nil, err
		}
		a.defaultConfig = source.Config
		a.defaultConfigPath = opts.ConfigFile
		a.configState.source = source
	}
	for i, overlay := range a.configState.overlays {
		bound, err := overlay.BindPaths(a.cwd)
		if err != nil {
			return nil, err
		}
		a.configState.overlays[i] = bound
	}
	if a.stdin == "" {
		a.stdin = "<stdin>"
	}
	return a, nil
}
func (a *Session) WorkingDir() string                      { return a.cwd }
func (a *Session) ProjectAt(path string) (*Project, error) { return a.projects.At(path) }
func (a *Session) selectionLog(args ...any) {
	a.log(args...)
}

// Repository analyzes the nearest repository's workflows and actions. An empty dir uses WorkingDir.
func (a *Session) Repository(dir string) (*Selection, error) {
	if dir == "" {
		dir = a.cwd
	}
	a.selectionLog("Linting workflows and actions in repository:", dir)
	project, err := a.projects.At(dir)
	if err != nil {
		return nil, err
	}
	if project == nil {
		if found, expected := discovery.WorkflowTypo(dir, files.New(a.ctx, nil, nil)); found != "" {
			return nil, fmt.Errorf("no workflows directory was found: %q looks misspelled; rename it to %q", found, expected)
		}
		return nil, fmt.Errorf("no project was found in any parent directories of %q. run actionlint inside a Git repository or provide explicit file paths", dir)
	}
	a.selectionLog("Detected project:", project.RootDir())
	if _, err := os.Stat(project.WorkflowsDir()); os.IsNotExist(err) {
		if found, expected := discovery.WorkflowTypo(dir, files.New(a.ctx, nil, nil)); found != "" {
			return nil, fmt.Errorf("no workflows directory was found: %q looks misspelled; rename it to %q", found, expected)
		}
	}
	metadata := &Inputs{}
	paths, err := discovery.Documents(a.ctx, project.RootDir(), files.New(a.ctx, nil, metadata.Add), discovery.Discovery{NoIgnore: a.noIgnore})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		a.filesSelected(paths)
		if found, expected := discovery.WorkflowTypo(dir, files.New(a.ctx, nil, nil)); found != "" {
			return nil, fmt.Errorf("no workflows directory was found: %q looks misspelled; rename it to %q", found, expected)
		}
		return nil, fmt.Errorf("no workflow or action files were found in %q", project.RootDir())
	}
	return a.discoveredFiles(paths, metadata.List(), project)
}

func (a *Session) Directory(dir string, project *Project) (*Selection, error) {
	listed := &Inputs{}
	paths, err := WorkflowPaths(files.New(a.ctx, nil, listed.Add), dir)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		a.filesSelected(paths)
		return nil, fmt.Errorf("no YAML file was found in %q", dir)
	}
	return a.discoveredFiles(paths, listed.List(), project)
}

func (a *Session) discoveredFiles(paths, metadata []string, project *Project) (*Selection, error) {
	a.selectionLog("Collected", len(paths), "YAML files")
	a.filesSelected(paths)
	result, err := a.readFiles(paths, project, nil)
	if result != nil {
		// Protect excluded discovery candidates from report replacement too.
		inputs := &Inputs{}
		for _, path := range result.Inputs {
			inputs.Add(path)
		}
		for _, path := range paths {
			inputs.Add(path)
		}
		for _, path := range metadata {
			inputs.Add(path)
		}
		result.Inputs = inputs.List()
	}
	return result, err
}

func (a *Session) filesSelected(paths []string) {
	if a.onFilesSelected != nil {
		a.onFilesSelected(slices.Clone(paths))
	}
}

// Files expands directory inputs and analyzes paths in order, discovering each
// file's project when project is nil. Overlapping inputs are deduplicated.
// Relative paths retain the caller's spelling and resolve from the process directory.
func (a *Session) Files(paths []string, project *Project) (*Selection, error) {
	selection, err := FindFiles(a.ctx, paths, a.noIgnore)
	if err != nil {
		return nil, err
	}
	return a.SelectedFiles(selection, project)
}

// SelectedFiles reads a discovery result without reinterpreting discovered paths
// as literal file arguments. Hosts can partition it by repository before loading plugins.
func (a *Session) SelectedFiles(selection FileSelection, project *Project) (*Selection, error) {
	paths := make([]string, 0, len(selection.Files))
	explicit := make(map[string]bool)
	for _, file := range selection.Files {
		paths = append(paths, file.Path)
		explicit[absPath(file.Path)] = file.Explicit
	}
	a.filesSelected(paths)
	if len(paths) == 0 {
		return &Selection{Sources: []Source{}, Inputs: selection.Inputs}, nil
	}
	result, err := a.readFiles(paths, project, explicit)
	if result != nil {
		result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, selection.Inputs...))))
	}
	return result, err
}

// Empty analyzes nothing. The result records the selected configuration of WorkingDir, explicit
// or from its project, as an input with its report.
func (a *Session) Empty() (*Selection, error) {
	project, err := a.projects.At(a.cwd)
	if err != nil {
		return nil, err
	}
	result := &Selection{Sources: []Source{}}
	inputs := &Inputs{}
	inputs.Add(a.defaultConfigPath)
	if project != nil || a.defaultConfig != nil {
		if _, err := a.ConfigForProject(project); err != nil {
			return nil, err
		}
		a.configState.Lock()
		inputs.AddConfig(a.configState.loaded[project], a.configState.provenance[project], project, a.cwd)
		result.Configurations = append(result.Configurations, configurationSnapshot(a.configState.reports[project]))
		a.configState.Unlock()
	}
	result.Inputs = inputs.List()
	return result, nil
}

func (a *Session) ReadFiles(paths []string, project *Project) (*Selection, error) {
	return a.readFiles(paths, project, nil)
}

func (a *Session) readFiles(paths []string, project *Project, explicit map[string]bool) (*Selection, error) {
	sources := make([]Source, 0, len(paths))
	configProjects := make([]*Project, 0, len(paths))
	for _, path := range paths {
		inputPath := path
		proj := project
		if proj == nil {
			var err error
			proj, err = a.projects.At(inputPath)
			if err != nil {
				return nil, err
			}
		}
		cfg, err := a.ConfigForProject(proj)
		if err != nil {
			return nil, err
		}
		configProjects = append(configProjects, proj)
		root := a.cwd
		if proj != nil {
			root = absPath(proj.RootDir())
		}
		fullPath := absPath(inputPath)
		if !cfg.IncludesSelectedFile(fullPath, root, a.noIgnore || explicit[fullPath]) {
			continue
		}
		content, err := a.readFile(inputPath)
		if err != nil {
			return nil, fmt.Errorf("could not read %q: %w", path, err)
		}
		displayPath := a.relativePath(fullPath)
		if !filepath.IsAbs(path) && filepath.Clean(path) == displayPath {
			displayPath = path
		}
		source := a.source(displayPath, content, proj)
		source.InputPath = fullPath
		source.ExplicitFile = explicit[fullPath]
		sources = append(sources, source)
	}
	return a.prepare(sources, configProjects...)
}

func (a *Session) relativePath(path string) string {
	if rel, err := filepath.Rel(a.cwd, path); err == nil {
		return rel
	}
	return path
}

// ReadStdin analyzes a reader using StdinFileName. normalizePath makes its diagnostic
// path relative to WorkingDir; false preserves the caller's spelling.
func (a *Session) ReadStdin(stdin io.Reader, normalizePath bool) (*Selection, error) {
	a.selectionLog("Reading the input from stdin")
	content, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("could not read stdin: %w", err)
	}
	return a.Content(a.stdin, content, nil, normalizePath)
}

func (a *Session) Content(path string, content []byte, project *Project, normalizePath bool) (*Selection, error) {
	if project == nil && path != "<stdin>" {
		discoveryPath := path
		if !filepath.IsAbs(discoveryPath) {
			discoveryPath = filepath.Join(a.cwd, discoveryPath)
		}
		if _, err := os.Stat(discoveryPath); !errors.Is(err, os.ErrNotExist) {
			var err error
			project, err = a.projects.At(discoveryPath)
			if err != nil {
				return nil, err
			}
		}
	}
	source := a.source(path, content, project)
	source.InputPath = path
	if normalizePath {
		source.Path = a.relativePath(path)
	}
	return a.prepare([]Source{source})
}

func (a *Session) source(path string, content []byte, project *Project) Source {
	cfg := a.defaultConfig
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	return Source{Path: path, Content: content, Config: cfg, Project: project}
}

func (a *Session) prepare(sources []Source, configProjects ...*Project) (*Selection, error) {
	for i := range sources {
		cfg, err := a.ConfigForProject(sources[i].Project)
		if err != nil {
			return nil, err
		}
		sources[i].Config = cfg
		sources[i].ConfigProvenance = a.ConfigProvenanceForProject(sources[i].Project)
	}
	// Report only projects used by this input selection.
	projects := make([]*Project, 0, len(sources)+len(configProjects))
	projects = append(projects, configProjects...)
	for _, source := range sources {
		projects = append(projects, source.Project)
	}
	result := &Selection{Sources: sources}
	inputs := &Inputs{}
	for _, source := range sources {
		path := source.InputPath
		if path != "" && path != "<stdin>" && !filepath.IsAbs(path) {
			path = filepath.Join(a.cwd, path)
		}
		inputs.Add(path)
	}
	inputs.Add(a.defaultConfigPath)
	// Keep provenance on the analysis itself, including cached selections when
	// a library caller reuses this session. Preserve input order across projects.
	a.configState.Lock()
	seen := make(map[*Project]bool)
	for _, project := range projects {
		if !seen[project] {
			seen[project] = true
			inputs.AddConfig(a.configState.loaded[project], a.configState.provenance[project], project, a.cwd)
			result.Configurations = append(result.Configurations, configurationSnapshot(a.configState.reports[project]))
		}
	}
	a.configState.Unlock()
	result.Inputs = inputs.List()
	return result, nil
}
