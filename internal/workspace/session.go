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

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/projectpath"
)

// Source associates the displayed path with the file read for analysis.
type Source struct {
	Path      string
	Content   []byte
	Config    *repoconfig.Config
	Project   *Project
	InputPath string
}

// Selection is a prepared batch. Inputs includes configuration and excluded discovery candidates.
type Selection struct {
	Sources        []Source
	Inputs         []string
	Configurations []repoconfig.Report
}

// Options controls only input preparation, not analysis or rendering.
type Options struct {
	Context                               context.Context
	WorkingDir, StdinFileName, ConfigFile string
	ConfigOverlays                        []repoconfig.Overlay
	OnConfigLoaded                        func(repoconfig.Report)
	SkipProjectConfig                     bool
	OnFilesSelected                       func([]string)
	ReadFile                              func(string) ([]byte, error)
	RuleNames                             []string
	Log                                   func(...any)
	LogWriter                             io.Writer
}

// Session shares repository and configuration lookups across input selections.
type Session struct {
	projects *Projects
	// DefaultConfig is an explicit in-memory configuration override for legacy callers.
	DefaultConfig     *repoconfig.Config
	defaultConfigPath string
	configState       *configState
	ctx               context.Context
	cwd, stdin        string
	onFilesSelected   func([]string)
	readFile          func(string) ([]byte, error)
	log               func(...any)
	logOut            io.Writer
	ruleNames         []string
}

func New(opts Options) (*Session, error) {
	a := &Session{projects: NewProjects(), ctx: opts.Context, cwd: opts.WorkingDir, stdin: opts.StdinFileName, onFilesSelected: opts.OnFilesSelected, readFile: opts.ReadFile, log: opts.Log, logOut: opts.LogWriter, ruleNames: slices.Clone(opts.RuleNames)}
	if a.ctx == nil {
		a.ctx = context.Background()
	}
	if a.readFile == nil {
		a.readFile = fsread.Disk
	}
	if a.log == nil {
		a.log = func(...any) {}
	}
	if a.logOut == nil {
		a.logOut = io.Discard
	}
	a.projects.readFile = a.readFile
	a.projects.ruleNames = a.ruleNames
	a.projects.skipConfig = opts.SkipProjectConfig
	a.configState = &configState{overlays: slices.Clone(opts.ConfigOverlays), onLoaded: opts.OnConfigLoaded, loaded: make(map[*Project]*repoconfig.Config), reports: make(map[*Project]repoconfig.Report)}
	if opts.ConfigFile != "" {
		source, err := repoconfig.Read(opts.ConfigFile, a.readFile, a.ruleNames...)
		if err != nil {
			return nil, err
		}
		a.DefaultConfig = source.Config
		a.defaultConfigPath = opts.ConfigFile
		a.configState.source = source
	}
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
		if found, expected := projectpath.WorkflowTypo(dir, fsread.New(a.ctx, nil, nil)); found != "" {
			return nil, fmt.Errorf("no workflows directory was found: %q looks misspelled; rename it to %q", found, expected)
		}
		return nil, fmt.Errorf("no project was found in any parent directories of %q. run actionlint inside a Git repository or provide explicit file paths", dir)
	}
	a.selectionLog("Detected project:", project.RootDir())
	if _, err := os.Stat(project.WorkflowsDir()); os.IsNotExist(err) {
		if found, expected := projectpath.WorkflowTypo(dir, fsread.New(a.ctx, nil, nil)); found != "" {
			return nil, fmt.Errorf("no workflows directory was found: %q looks misspelled; rename it to %q", found, expected)
		}
	}
	metadata := &Inputs{}
	paths, err := RepositoryPaths(a.ctx, project, fsread.New(a.ctx, nil, metadata.Add))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		a.filesSelected(paths)
		if found, expected := projectpath.WorkflowTypo(dir, fsread.New(a.ctx, nil, nil)); found != "" {
			return nil, fmt.Errorf("no workflows directory was found: %q looks misspelled; rename it to %q", found, expected)
		}
		return nil, fmt.Errorf("no workflow or action files were found in %q", project.RootDir())
	}
	return a.discoveredFiles(paths, metadata.List(), project)
}

func (a *Session) Directory(dir string, project *Project) (*Selection, error) {
	listed := &Inputs{}
	paths, err := WorkflowPaths(fsread.New(a.ctx, nil, listed.Add), dir)
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
	result, err := a.Files(paths, project)
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

// Files analyzes paths in order, discovering each file's project when project is nil.
func (a *Session) Files(paths []string, project *Project) (*Selection, error) {
	a.filesSelected(paths)
	if len(paths) == 0 {
		return &Selection{Sources: []Source{}}, nil
	}
	if len(paths) > 1 {
		a.selectionLog("Linting", len(paths), "files")
	}
	return a.ReadFiles(paths, project)
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
	if project != nil || a.DefaultConfig != nil {
		if _, err := a.ConfigForProject(project); err != nil {
			return nil, err
		}
		a.configState.Lock()
		inputs.AddConfig(a.configState.loaded[project], project, a.cwd)
		result.Configurations = append(result.Configurations, a.configState.reports[project])
		a.configState.Unlock()
	}
	result.Inputs = inputs.List()
	return result, nil
}

func (a *Session) ReadFiles(paths []string, project *Project) (*Selection, error) {
	sources := make([]Source, 0, len(paths))
	configProjects := make([]*Project, 0, len(paths))
	for _, path := range paths {
		proj := project
		if proj == nil {
			var err error
			proj, err = a.projects.At(path)
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
		fullPath := absPath(path)
		if !cfg.IncludesFile(fullPath, root) {
			continue
		}
		content, err := a.readFile(path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q: %w", path, err)
		}
		displayPath := a.relativePath(fullPath)
		if !filepath.IsAbs(path) && filepath.Clean(path) == displayPath {
			displayPath = path
		}
		source := a.source(displayPath, content, proj)
		source.InputPath = fullPath
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
	cfg := a.DefaultConfig
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
			inputs.AddConfig(a.configState.loaded[project], project, a.cwd)
			result.Configurations = append(result.Configurations, a.configState.reports[project])
		}
	}
	a.configState.Unlock()
	result.Inputs = inputs.List()
	return result, nil
}
