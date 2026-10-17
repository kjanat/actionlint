package engine

import (
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/internal/workspace"
)

type Project = workspace.Project
type Projects = workspace.Projects

func NewProject(root string) (*Project, error) { return workspace.NewProject(root) }
func NewProjects() *Projects                   { return workspace.NewProjects() }
func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}

// physicalSourcePath also resolves aliases above an in-memory file which does
// not exist yet. Failures other than missing paths retain the original spelling.
func physicalSourcePath(path string) string {
	path = absPath(path)
	parent, suffix := path, ""
	for {
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(resolved, suffix)
		} else if !os.IsNotExist(err) {
			return path
		}
		next := filepath.Dir(parent)
		if next == parent {
			return path
		}
		suffix = filepath.Join(filepath.Base(parent), suffix)
		parent = next
	}
}

func projectRoot(p *Project) string {
	if p == nil {
		return ""
	}
	return p.RootDir()
}
