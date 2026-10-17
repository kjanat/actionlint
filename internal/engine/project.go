package engine

import (
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

func projectRoot(p *Project) string {
	if p == nil {
		return ""
	}
	return p.RootDir()
}
