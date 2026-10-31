package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/config/address"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
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
	remote := address.Remote(path)
	if remote {
		if err := ValidateSourcePaths(path, node); err != nil {
			return nil, nil, err
		}
	} else {
		toolconfig.NormalizePaths(node, filepath.Dir(path))
	}
	// Preserve the declaring origin of inherited relative tool configuration
	// paths. A "*" key matches every entry of a mapping.
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
				if keys[0] == "*" || n.Content[i].Value == keys[0] {
					visit(n.Content[i+1], keys[1:])
				}
			}
		}
	}
	if inherited && !remote {
		visit(node, []string{"tools", "*", "config"})
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				if node.Content[i].Value == "overrides" {
					for _, override := range node.Content[i+1].Content {
						visit(override, []string{"tools", "*", "config"})
					}
				}
			}
		}
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
	return normalizeToolSwitch(node), resolved.Warnings, nil
}

// ValidateSourcePaths rejects settings that require a declaring local directory
// in an HTTPS source. Plugin discovery calls this before its partial projection.
func ValidateSourcePaths(origin string, node *yaml.Node) error {
	if !address.Remote(origin) {
		return nil
	}
	if err := validateRemoteToolPaths(node); err != nil {
		return err
	}
	if plugins := field(node, "plugins"); plugins != nil {
		for _, entry := range plugins.Content {
			if registry := field(entry, "registry"); registry != nil && strings.HasPrefix(strings.ToLower(registry.Value), "file:") {
				return fmt.Errorf("plugins.registry: remote config cannot select a local file registry")
			}
		}
	}
	return nil
}

// Remote declarations have no local config directory. Repository paths and
// runtime workspace expressions retain their existing local meaning.
func validateRemoteToolPaths(node *yaml.Node) error {
	if tools := field(node, "tools"); tools != nil && tools.Kind == yaml.MappingNode {
		for i := 0; i < len(tools.Content); i += 2 {
			name, entry := tools.Content[i].Value, tools.Content[i+1]
			settings := field(entry, "config")
			if err := rejectRemoteConfigDir(settings); err != nil {
				return fmt.Errorf("tools.%s.config: %w", name, err)
			}
			if settings != nil && settings.Kind == yaml.ScalarNode && settings.ShortTag() == "!!str" && !filepath.IsAbs(settings.Value) && !runtimeAbsolutePath(settings.Value) {
				return fmt.Errorf("tools.%s.config: remote config cannot declare a relative filesystem path; use an absolute path or a workspace path expression", name)
			}
			backend := field(entry, "backend")
			if executable := field(field(backend, "process"), "executable"); executable != nil {
				if err := rejectRemoteConfigDir(executable); err != nil {
					return fmt.Errorf("tools.%s.backend.process.executable: %w", name, err)
				}
				if !filepath.IsAbs(executable.Value) && (strings.ContainsAny(executable.Value, `/\`) || filepath.VolumeName(executable.Value) != "" || executable.Value == "." || executable.Value == "..") {
					return fmt.Errorf("tools.%s.backend.process.executable: remote config cannot declare a relative filesystem path", name)
				}
			}
			if module := field(field(backend, "wasm"), "path"); module != nil {
				if err := rejectRemoteConfigDir(module); err != nil {
					return fmt.Errorf("tools.%s.backend.wasm.path: %w", name, err)
				}
				if !filepath.IsAbs(module.Value) && !strings.Contains(module.Value, "://") {
					return fmt.Errorf("tools.%s.backend.wasm.path: remote config cannot declare a relative filesystem path", name)
				}
			}
		}
	}
	if overrides := field(node, "overrides"); overrides != nil {
		for _, override := range overrides.Content {
			if err := validateRemoteToolPaths(override); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectRemoteConfigDir(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	value := node.Value
	for {
		_, expression, found := strings.Cut(value, "${{")
		if !found {
			break
		}
		name, after, closed := strings.Cut(expression, "}}")
		if !closed {
			break
		}
		if strings.EqualFold(strings.TrimSpace(name), "configdir") {
			return fmt.Errorf("configdir is unavailable in a remote config; use github.workspace, gitdir or github.action_path")
		}
		value = after
	}
	for _, child := range node.Content {
		if err := rejectRemoteConfigDir(child); err != nil {
			return err
		}
	}
	return nil
}

func runtimeAbsolutePath(value string) bool {
	expression, found := strings.CutPrefix(value, "${{")
	if !found {
		return false
	}
	name, _, closed := strings.Cut(expression, "}}")
	if !closed {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "gitdir", "github.workspace", "github.action_path":
		return true
	default:
		return false
	}
}

func expandInheritedConfigDirectory(value, directory string) string {
	context := PathContext{ConfigDir: directory}
	var out strings.Builder
	for {
		before, expression, found := strings.Cut(value, "${{")
		out.WriteString(before)
		if !found {
			return out.String()
		}
		name, after, closed := strings.Cut(expression, "}}")
		if !closed {
			out.WriteString("${{")
			out.WriteString(expression)
			return out.String()
		}
		token := "${{" + name + "}}"
		// Use runtime name normalization, leaving analysis-context paths late-bound.
		if expanded, err := context.Expand(token); err == nil {
			out.WriteString(expanded)
		} else {
			out.WriteString(token)
		}
		value = after
	}
}
