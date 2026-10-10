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
	if filepath.IsAbs(path) && root != "" {
		if relative, err := filepath.Rel(root, path); err == nil {
			path = relative
		}
	}
	return cfg.Files.Match(path)
}

func normalizeExtendedConfig(path string, node *yaml.Node, inherited bool) (*yaml.Node, error) {
	// Validate each source independently, so overridden bad settings still fail.
	if _, err := resolveConfigNode(node, nil); err != nil {
		return nil, err
	}
	// Preserve the declaring origin of inherited relative ShellCheck rc paths.
	var visit func(*yaml.Node, []string)
	visit = func(n *yaml.Node, keys []string) {
		if len(keys) == 0 {
			if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
				value := strings.ReplaceAll(n.Value, "${{ configdir }}", filepath.Dir(path))
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
	return normalizeToolSwitch(node), nil
}
