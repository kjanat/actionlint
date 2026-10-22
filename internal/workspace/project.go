package workspace

import (
	"os"
	"path/filepath"
	"strings"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/fsread"
)

// Project represents one GitHub project. One Git repository corresponds to one project.
type Project struct {
	root       string
	config     *repoconfig.Loaded
	configPath string
}

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		path = p
	}
	return path
}

// Cache only paths actually visited: an unvisited descendant may be a nested
// repository even when its ancestor already has a cached project.
func cachedProjectRoot(path string, roots map[string]string, stat func(string) (os.FileInfo, error)) string {
	d := absPath(path)
	var visited []string
	remember := func(root string) string {
		for _, path := range visited {
			roots[path] = root
		}
		return root
	}
	for {
		if root, ok := roots[d]; ok {
			return remember(root)
		}
		if roots != nil {
			visited = append(visited, d)
		}
		if _, err := stat(filepath.Join(d, ".git")); err == nil { // Note: .git may be a file
			return remember(d)
		}

		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// Identity creates a repository identity without loading configuration.
func Identity(root string) *Project { return &Project{root: root} }

// ConfigPath returns the selected repository configuration path.
func (p *Project) ConfigPath() string { return p.configPath }

// NewProject creates a new instance with a file path to the root directory of the repository.
// This function returns an error when failing to parse an actionlint config file in the repository.
func NewProject(root string) (*Project, error) {
	return newProject(root, fsread.Disk)
}

func newProject(root string, readFile func(string) ([]byte, error), additional ...string) (*Project, error) {
	c, path, err := repoconfig.LoadRepository(root, readFile, additional...)
	if err != nil {
		return nil, err
	}
	return &Project{root: root, config: c, configPath: path}, nil
}

// RootDir returns a root directory path of the GitHub project repository.
func (p *Project) RootDir() string {
	return p.root
}

// WorkflowsDir returns a ".github/workflows" directory path of the GitHub project repository.
// This method does not check if the directory exists.
func (p *Project) WorkflowsDir() string {
	return filepath.Join(p.root, ".github", "workflows")
}

// Knows returns true when the project knows the given file. When a file is included in the
// project's directory, the project knows the file.
func (p *Project) Knows(path string) bool {
	rel, err := filepath.Rel(absPath(p.root), absPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Config returns config object of the GitHub project repository. The config file was read from
// ".github/actionlint.yaml" or ".github/actionlint.yml" when this Project instance was created.
// When no config was found, this method returns nil.
func (p *Project) Config() *repoconfig.Config {
	// Note: Calling this method must be thread safe (#333)
	if p.config == nil {
		return nil
	}
	return p.config.Config
}

// Projects represents set of projects. It caches Project instances which was created previously
// and reuses them.
// Create a new instance after changing repository boundaries or loaded configuration.
type Projects struct {
	ruleNames  []string
	known      []*Project
	roots      map[string]string
	skipConfig bool
	readFile   func(string) ([]byte, error)
}

// NewProjects creates new Projects instance.
func NewProjects() *Projects {
	return &Projects{}
}

// At returns the Project instance which the path belongs to. It returns nil if no project is found
// from the path.
func (ps *Projects) At(path string) (*Project, error) {
	// Discover the nearest repository before consulting cached configurations.
	// A cached ancestor must not hide a nested repository or Git worktree.
	if ps.roots == nil {
		ps.roots = make(map[string]string)
	}
	root := cachedProjectRoot(path, ps.roots, os.Stat)
	if root == "" {
		return nil, nil
	}
	for _, p := range ps.known {
		if p.root == root {
			return p, nil
		}
	}

	readFile := ps.readFile
	if readFile == nil {
		readFile = fsread.Disk
	}
	p := &Project{root: root}
	if !ps.skipConfig {
		var err error
		p, err = newProject(root, readFile, ps.ruleNames...)
		if err != nil {
			return nil, err
		}
	}
	ps.known = append(ps.known, p)

	return p, nil
}

// Discoverer selects configuration loading policy before the first lookup.
func Discoverer(skipConfig bool) *Projects { return &Projects{skipConfig: skipConfig} }
