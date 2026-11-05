package checking

import (
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}

func projectRoot(p *workspace.Project) string {
	if p == nil {
		return ""
	}
	return p.RootDir()
}
