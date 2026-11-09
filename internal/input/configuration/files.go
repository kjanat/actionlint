package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/selection"
)

// FilesConfig is the shared include/exclude scope for input files.
type FilesConfig = selection.Selection

func (cfg *Config) IncludesFile(path, root string) bool {
	if cfg == nil {
		return true
	}
	return cfg.Files.Match(RepositoryRelativePath(path, root))
}

// IncludesSelectedFile applies configured file filters unless bypassFilters is set.
func (cfg *Config) IncludesSelectedFile(path, root string, bypassFilters bool) bool {
	return bypassFilters || cfg.IncludesFile(path, root)
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

func (ruleSet RuleSet) normalizeExtendedConfig(path string, value any, inputs map[string]Input, inherited, bindRules bool, capture func(any), additional ...string) (any, []Warning, error) {
	resolved, err := ruleSet.resolveValue(value, inputs, bindRules, additional...)
	if err != nil {
		return nil, nil, err
	}
	value = resolved.Value
	for i := range resolved.Warnings {
		resolved.Warnings[i].File = path
	}
	if address.Remote(path) {
		if err := ValidateSourcePaths(path, value); err != nil {
			return nil, nil, err
		}
	} else {
		normalizeSourcePaths(value, filepath.Dir(path), inherited)
	}
	capture(value)
	if local := field(value, "local-actions"); local != nil {
		normalizeLocalActionSourceNode(local)
	}
	if lint := field(value, "lint"); lint != nil {
		lint, err := ruleSet.normalizeRuleLocations(lint)
		if err != nil {
			return nil, nil, err
		}
		setField(value, "lint", ruleSet.expandLintShorthands(lint))
	}
	return normalizeToolSwitch(value), resolved.Warnings, nil
}

func normalizeSourcePaths(value any, directory string, inherited bool) {
	for _, entry := range members(field(value, "tools")) {
		backend := field(entry, "backend")
		for _, item := range []struct {
			parent     any
			key        string
			executable bool
		}{
			{field(backend, "process"), "executable", true}, {field(backend, "wasm"), "path", false},
		} {
			path, ok := field(item.parent, item.key).(string)
			if ok && path != "" && !filepath.IsAbs(path) && ((item.executable && strings.ContainsAny(path, `/\`)) || (!item.executable && !strings.Contains(path, "://"))) {
				setField(item.parent, item.key, filepath.Join(directory, path))
			}
		}
		if inherited {
			path, ok := field(entry, "config").(string)
			if ok {
				path = expandInheritedConfigDirectory(path, directory)
				if !filepath.IsAbs(path) && !strings.Contains(path, "${{") {
					path = filepath.Join(directory, path)
				}
				setField(entry, "config", path)
			}
		}
	}
	for _, child := range elements(field(value, "overrides")) {
		normalizeSourcePaths(child, directory, inherited)
	}
}

// ValidateSourcePaths rejects local paths in an HTTPS source.
func ValidateSourcePaths(origin string, value any) error {
	if !address.Remote(origin) {
		return nil
	}
	if err := validateRemoteToolPaths(value); err != nil {
		return err
	}
	for _, entry := range elements(field(value, "plugins")) {
		if strings.HasPrefix(strings.ToLower(text(field(entry, "registry"))), "file:") {
			return errors.New("plugins.registry: remote config cannot select a local file registry")
		}
	}
	return nil
}

func validateRemoteToolPaths(value any) error {
	for name, entry := range members(field(value, "tools")) {
		settings := field(entry, "config")
		if err := rejectRemoteConfigDir(settings); err != nil {
			return fmt.Errorf("tools.%s.config: %w", name, err)
		}
		if path, ok := settings.(string); ok && !filepath.IsAbs(path) && !runtimeAbsolutePath(path) {
			return fmt.Errorf("tools.%s.config: remote config cannot declare a relative filesystem path; use an absolute path or a workspace path expression", name)
		}
		backend := field(entry, "backend")
		for _, item := range []struct {
			value      any
			label      string
			executable bool
		}{{field(field(backend, "process"), "executable"), "backend.process.executable", true}, {field(field(backend, "wasm"), "path"), "backend.wasm.path", false}} {
			if item.value == nil {
				continue
			}
			if err := rejectRemoteConfigDir(item.value); err != nil {
				return fmt.Errorf("tools.%s.%s: %w", name, item.label, err)
			}
			path := text(item.value)
			if !filepath.IsAbs(path) && ((item.executable && (strings.ContainsAny(path, `/\`) || filepath.VolumeName(path) != "" || path == "." || path == "..")) || (!item.executable && !strings.Contains(path, "://"))) {
				return fmt.Errorf("tools.%s.%s: remote config cannot declare a relative filesystem path", name, item.label)
			}
		}
	}
	for _, child := range elements(field(value, "overrides")) {
		if err := validateRemoteToolPaths(child); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemoteConfigDir(value any) error {
	switch value := value.(type) {
	case string:
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
				return errors.New("configdir is unavailable in a remote config; use github.workspace, gitdir or github.action_path")
			}
			value = after
		}
	case map[string]any:
		for _, child := range value {
			if err := rejectRemoteConfigDir(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := rejectRemoteConfigDir(child); err != nil {
				return err
			}
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

// SourceError identifies the source location of a configuration error.
type SourceError struct {
	File         string
	Line, Column int
	Cause        error
}

func (e *SourceError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %v", e.File, e.Line, e.Column, e.Cause)
	}
	return fmt.Sprintf("line %d, column %d: %v", e.Line, e.Column, e.Cause)
}
func (e *SourceError) Unwrap() error { return e.Cause }
func sourceError(err error, inputs map[string]Input) error {
	if err == nil {
		return nil
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var failures []error
		for _, failure := range group.Unwrap() {
			failures = append(failures, sourceError(failure, inputs))
		}
		return errors.Join(failures...)
	}
	var bindingError *binding.Error
	if !errors.As(err, &bindingError) {
		return err
	}
	pointer := bindingError.Path
	for {
		if input, ok := inputs[pointer]; ok && input.Line > 0 {
			line, column := input.Line, input.Column
			if errors.Is(err, binding.ErrUnknownKey) && input.KeyLine > 0 {
				line, column = input.KeyLine, input.KeyColumn
			}
			return &SourceError{File: input.File, Line: line, Column: column, Cause: err}
		}
		at := strings.LastIndex(pointer, "/")
		if at < 0 {
			break
		}
		pointer = pointer[:at]
	}
	return err
}
