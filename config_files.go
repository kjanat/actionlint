package actionlint

import (
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/internal/filefilter"
	"go.yaml.in/yaml/v4"
)

// FilesConfig is the shared include/exclude scope for input files.
type FilesConfig = filefilter.Selection

func (cfg *Config) includesFile(path, root string) bool {
	if cfg == nil {
		return true
	}
	return cfg.Files.Match(repositoryRelativeConfigPath(path, root))
}

func repositoryRelativeConfigPath(path, root string) string {
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

func normalizeExtendedConfig(path string, node *yaml.Node, inherited bool) (*yaml.Node, []ConfigWarning, error) {
	// Validate each source independently, so overridden bad settings still fail.
	resolved, err := resolveConfigNode(node, nil)
	if err != nil {
		return nil, nil, err
	}
	for i := range resolved.warnings {
		resolved.warnings[i].File = path
	}
	// Preserve the declaring origin of inherited relative ShellCheck rc paths.
	var visit func(*yaml.Node, []string)
	visit = func(n *yaml.Node, keys []string) {
		if len(keys) == 0 {
			if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
				value := expandInheritedConfigDirectory(n.Value, filepath.Dir(path))
				if !filepath.IsAbs(value) && !strings.Contains(value, "${{") {
					value = filepath.Join(filepath.Dir(path), value)
				}
				n.Value = value
			}
			return
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Content); i += 2 {
				if n.Content[i].Value == keys[0] {
					visit(n.Content[i+1], keys[1:])
				}
			}
		}
	}
	if inherited {
		visit(node, []string{"tools", "shellcheck", "config"})
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				if node.Content[i].Value == "overrides" {
					for _, override := range node.Content[i+1].Content {
						visit(override, []string{"tools", "shellcheck", "config"})
					}
				}
			}
		}
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "lint" {
				node.Content[i+1] = expandLintShorthands(node.Content[i+1])
			}
		}
	}
	return normalizeToolSwitch(node), resolved.warnings, nil
}

func expandInheritedConfigDirectory(value, directory string) string {
	context := configPathContext{configDir: directory}
	var out strings.Builder
	for {
		before, expression, found := strings.Cut(value, "${{")
		out.WriteString(before)
		if !found {
			return out.String()
		}
		name, after, closed := strings.Cut(expression, "}}")
		if !closed {
			out.WriteString("${{" + expression)
			return out.String()
		}
		token := "${{" + name + "}}"
		// Use runtime name normalization, leaving analysis-context paths late-bound.
		if expanded, err := context.expand(token); err == nil {
			out.WriteString(expanded)
		} else {
			out.WriteString(token)
		}
		value = after
	}
}
