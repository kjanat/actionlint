package config

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	"github.com/kjanat/go-yaml/v4"
	"github.com/pelletier/go-toml/v2"
)

// Generate creates the repository configuration without producing terminal output.
// skipConfig avoids parsing an existing configuration before checking for a collision.
// Reading the existing configuration stops when ctx is canceled.
func (ruleSet RuleSet) Generate(ctx context.Context, root string, skipConfig bool, version string) (string, error) {
	if root == "" {
		return "", errors.New("project is not found. check current project is initialized as Git repository and \".github/workflows\" directory exists")
	}
	if !skipConfig {
		if _, _, err := ruleSet.LoadRepositoryContext(ctx, root, nil); err != nil {
			return "", err
		}
	}
	directory := filepath.Join(root, ".github")
	for _, name := range RepositoryConfigNames {
		path := filepath.Join(directory, name)
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("config file already exists at %q", path)
		}
	}
	path := filepath.Join(directory, "actionlint.yaml")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("could not create config directory %q: %w", directory, err)
	}
	if err := ruleSet.WriteDefaultFile(path, version); err != nil {
		return "", err
	}
	return path, nil
}

//go:embed config_default.yaml
var defaultConfigTemplate string

func (ruleSet RuleSet) WriteDefaultFile(path, version string) error {
	b, err := ruleSet.DefaultDocument(version, FileFormat(strings.TrimPrefix(filepath.Ext(path), ".")))
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}

// DefaultDocument generates rule examples from this catalogue without writing a file.
func (ruleSet RuleSet) DefaultDocument(version string, format FileFormat) ([]byte, error) {
	if format == "" {
		format = FormatYAML
	}
	if !slices.Contains(FileFormats(), format) {
		return nil, fmt.Errorf("unsupported configuration format %q", format)
	}
	info, _ := debug.ReadBuildInfo()
	ref := buildinfo.SourceRef(version, info)
	schemaURL := "https://cdn.jsdelivr.net/gh/kjanat/actionlint@" + strings.TrimPrefix(ref, "v") + "/actionlint.schema.json"
	defaults, err := ruleSet.Parse([]byte(defaultConfigTemplate))
	if err != nil {
		return nil, fmt.Errorf("read default configuration: %w", err)
	}
	groups := make(map[string]map[string]Level)
	entries := ruleSet.Rules()
	slices.SortFunc(entries, func(a, b Rule) int { return strings.Compare(a.Name, b.Name) })
	for _, entry := range entries {
		if groups[entry.Category] == nil {
			groups[entry.Category] = make(map[string]Level)
		}
		groups[entry.Category][entry.Name] = ruleSet.DiagnosticLevel(defaults, entry.Name)
	}
	examples, err := commentedYAML(groups, "    # ")
	if err != nil {
		return nil, fmt.Errorf("generate rule reference: %w", err)
	}
	var override string
	if len(entries) != 0 {
		entry := entries[0]
		type lintExample struct {
			Rules map[string]map[string]Level `yaml:"rules"`
		}
		override, err = commentedYAML(map[string]lintExample{
			"lint": {Rules: map[string]map[string]Level{entry.Category: {entry.Name: "off"}}},
		}, "#     ")
		if err != nil {
			return nil, fmt.Errorf("generate rule override example: %w", err)
		}
	}
	levels := make([]string, 0, len(lintconfig.Levels()))
	for _, level := range lintconfig.Levels() {
		levels = append(levels, string(level))
	}
	replacer := strings.NewReplacer(
		"{{SCHEMA_URL}}", schemaURL,
		"{{RULE_LEVELS}}", strings.Join(levels, ", "),
		"    # {{RULE_EXAMPLES}}", examples,
		"#     {{RULE_OVERRIDE}}", override,
	)
	document := []byte(replacer.Replace(defaultConfigTemplate))
	if format == FormatYAML || format == FormatYML {
		return document, nil
	}
	var values map[string]any
	if err := yaml.Unmarshal(document, &values); err != nil {
		return nil, err
	}
	reference := map[string]any{"lint": map[string]any{"rules": groups}}
	if format == FormatTOML {
		active, err := toml.Marshal(values)
		if err != nil {
			return nil, err
		}
		examples, err := toml.Marshal(reference)
		if err != nil {
			return nil, err
		}
		return []byte("#:schema " + schemaURL + "\n\n" + string(active) + "\n# Rule defaults in this build:\n" + commentLines(string(examples), "# ") + "\n"), nil
	}
	values["$schema"] = schemaURL
	active, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, err
	}
	if format == FormatJSONC {
		examples, err := json.MarshalIndent(reference, "", "  ")
		if err != nil {
			return nil, err
		}
		active = append(active, []byte("\n\n// Rule defaults in this build:\n"+commentLines(string(examples), "// "))...)
	}
	return append(active, '\n'), nil
}

func commentedYAML(value any, prefix string) (string, error) {
	var content strings.Builder
	encoder := yaml.NewEncoder(&content)
	encoder.SetIndent(2)
	if err := errors.Join(encoder.Encode(value), encoder.Close()); err != nil {
		return "", err
	}
	return commentLines(content.String(), prefix), nil
}

func commentLines(content, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimSuffix(content, "\n"), "\n", "\n"+prefix)
}
