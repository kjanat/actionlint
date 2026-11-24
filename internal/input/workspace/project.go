package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

type Project struct {
	root       string
	config     *configuration.LoadedFile
	configPath string
}

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		path = p
	}
	return path
}

// Cache only paths actually visited. An unvisited descendant may be a nested
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

func (p *Project) ConfigPath() string { return p.configPath }

func NewProject(rules configuration.RuleSet, root string) (*Project, error) {
	return newProject(context.Background(), rules, root, nil, nil)
}

func newProject(ctx context.Context, rules configuration.RuleSet, root string, readFile func(string) ([]byte, error), configSources *document.Files, additional ...string) (*Project, error) {
	c, path, err := rules.LoadRepositoryContext(ctx, root, readFile, configSources, additional...)
	if err != nil {
		return nil, err
	}
	return &Project{root: root, config: c, configPath: path}, nil
}

func (p *Project) RootDir() string {
	return p.root
}

// WorkflowsDir does not check whether the directory exists.
func (p *Project) WorkflowsDir() string {
	return filepath.Join(p.root, ".github", "workflows")
}

func (p *Project) Knows(path string) bool {
	rel, err := filepath.Rel(absPath(p.root), absPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Config returns nil when the repository has no configuration file.
func (p *Project) Config() *configuration.Config {
	// Note: Calling this method must be thread safe (#333)
	if p.config == nil {
		return nil
	}
	return p.config.Config
}

func (p *Project) ConfigProvenance() configuration.Provenance {
	if p.config == nil {
		return configuration.Provenance{}
	}
	return p.config.Provenance
}

// Projects caches Project instances. Create a new Projects after changing repository boundaries or loaded configuration.
type Projects struct {
	rules         configuration.RuleSet
	ctx           context.Context
	ruleNames     []string
	known         []*Project
	roots         map[string]string
	skipConfig    bool
	readFile      func(string) ([]byte, error)
	configSources *document.Files
}

func NewProjects(rules configuration.RuleSet) *Projects {
	return &Projects{rules: rules}
}

// At returns nil when no project contains the path.
func (ps *Projects) At(path string) (*Project, error) {
	if ps.ctx == nil {
		ps.ctx = context.Background()
	}
	if err := ps.ctx.Err(); err != nil {
		return nil, err
	}
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
	p := &Project{root: root}
	if !ps.skipConfig {
		var err error
		p, err = newProject(ps.ctx, ps.rules, root, readFile, ps.configSources, ps.ruleNames...)
		if err != nil {
			return nil, err
		}
	}
	ps.known = append(ps.known, p)

	return p, nil
}

func Discoverer(rules configuration.RuleSet, skipConfig bool) *Projects {
	return &Projects{rules: rules, skipConfig: skipConfig}
}
