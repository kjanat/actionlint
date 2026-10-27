package config

import (
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/filefilter"

	"github.com/kjanat/go-yaml/v4"
)

// FilesConfig is the shared include/exclude scope for input files.
type FilesConfig = filefilter.Selection

func (cfg *Config) IncludesFile(path, root string) bool {
	if cfg == nil {
		return true
	}
	return cfg.Files.Match(RepositoryRelativePath(path, root))
}

func RepositoryRelativePath(path, root string) string {
	if !filepath.IsAbs(path) || root == "" {
		return path
	}
	root = absPath(root)
	relative, err := filepath.Rel(root, path)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return relative
	}
	// Preserve in-repository alias scopes. Resolve checkout aliases only when
	// lexical matching would place the supplied file outside the project.
	physicalRoot, rootErr := filepath.EvalSymlinks(root)
	physicalPath, pathErr := filepath.EvalSymlinks(path)
	if rootErr == nil && pathErr == nil {
		if physicalRelative, err := filepath.Rel(physicalRoot, physicalPath); err == nil {
			return physicalRelative
		}
	}
	if err == nil {
		return relative
	}
	return path
}

func normalizeExtendedConfig(path string, node *yaml.Node, inherited bool, additional ...string) (*yaml.Node, []Warning, error) {
	// Validate each source independently, so overridden bad settings still fail.
	resolved, err := ResolveNode(node, nil, additional...)
	if err != nil {
		return nil, nil, err
	}
	node = resolved.Node
	for i := range resolved.Warnings {
		resolved.Warnings[i].File = path
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "local-actions" {
				normalizeLocalActionSourceNode(node.Content[i+1])
			}
			if node.Content[i].Value == "lint" {
				node.Content[i+1] = expandLintShorthands(node.Content[i+1])
			}
		}
	}
	return node, resolved.Warnings, nil
}
