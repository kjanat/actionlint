package actionlint

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

var shellcheckCodeSelector = regexp.MustCompile(`^(all|(SC)?[0-9]+(-(SC)?[0-9]+)?)$`)
var shellcheckOptionalName = regexp.MustCompile(`^[a-zA-Z-]+$`)

// Normalize the boolean shorthand before merging so it only overrides enabled.
// Expanded YAML nodes retain their source positions, including alias/merge origins.
func normalizeToolSwitch(node *yaml.Node) *yaml.Node {
	var rewrite func(*yaml.Node, []string) *yaml.Node
	rewrite = func(node *yaml.Node, keys []string) *yaml.Node {
		if node == nil {
			return nil
		}
		normalized := *node
		if len(keys) == 0 {
			if node.Tag != "!!bool" {
				return node
			}
			normalized.Kind, normalized.Tag, normalized.Value = yaml.MappingNode, "!!map", ""
			normalized.Content = []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled", Line: node.Line, Column: node.Column}, node}
			return &normalized
		}
		normalized.Content = append([]*yaml.Node(nil), node.Content...)
		switch node.Kind {
		case yaml.DocumentNode:
			if len(normalized.Content) == 1 {
				normalized.Content[0] = rewrite(normalized.Content[0], keys)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(normalized.Content); i += 2 {
				if normalized.Content[i].Value == keys[0] {
					normalized.Content[i+1] = rewrite(normalized.Content[i+1], keys[1:])
				}
			}
		}
		return &normalized
	}
	return rewrite(node, []string{"tools", "shellcheck"})
}

func validateShellcheckBooleans(node *yaml.Node, keys ...string) error {
	var values map[string]yaml.Node
	if err := node.Decode(&values); err != nil {
		return err
	}
	for _, key := range keys {
		value, exists := values[key]
		if exists && value.Tag != "!!bool" && value.Tag != "!!null" {
			return fmt.Errorf("tools.shellcheck: %s must be a boolean at line %d", key, value.Line)
		}
	}
	return nil
}

// UnmarshalYAML validates the tool switch without YAML 1.1 string coercion.
func (config *ShellcheckToolConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return err
		}
		*config = ShellcheckToolConfig{Enabled: &enabled}
		return nil
	}
	type plain ShellcheckToolConfig
	var decoded plain
	if err := node.Load(&decoded, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if err := validateShellcheckBooleans(node, "enabled"); err != nil {
		return err
	}
	*config = ShellcheckToolConfig(decoded)
	return nil
}

// UnmarshalYAML selects a path or an inline mapping without conflating their fields.
func (config *ShellcheckConfigSource) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		if node.Value == "" || strings.ContainsAny(node.Value, "\x00\r\n") {
			return errors.New("tools.shellcheck.config: expected a nonempty single-line path")
		}
		config.value = shellcheckConfigPath(node.Value)
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("tools.shellcheck.config must be a path or mapping")
	}
	var inline ShellcheckConfig
	if err := node.Decode(&inline); err != nil {
		return err
	}
	config.value = shellcheckInlineConfig{inline}
	return nil
}

// MarshalYAML preserves the selected path or inline mapping in resolved output.
func (config *ShellcheckConfigSource) MarshalYAML() (any, error) {
	if config == nil {
		return nil, nil
	}
	switch value := config.value.(type) {
	case shellcheckConfigPath:
		return string(value), nil
	case shellcheckInlineConfig:
		return value.ShellcheckConfig, nil
	default:
		return nil, nil
	}
}

func (config *ShellcheckConfigSource) inline() *ShellcheckConfig {
	if config != nil {
		if value, ok := config.value.(shellcheckInlineConfig); ok {
			return &value.ShellcheckConfig
		}
	}
	return nil
}

func (config *ShellcheckConfigSource) path() string {
	if config != nil {
		if value, ok := config.value.(shellcheckConfigPath); ok {
			return string(value)
		}
	}
	return ""
}

// UnmarshalYAML rejects misspelled tool settings, including unknown nested keys.
func (config *ToolsConfig) UnmarshalYAML(node *yaml.Node) error {
	type plain ToolsConfig
	var decoded plain
	if err := node.Load(&decoded, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	*config = ToolsConfig(decoded)
	return nil
}

// UnmarshalYAML validates native directive values before analysis starts.
func (config *ShellcheckConfig) UnmarshalYAML(node *yaml.Node) error {
	type plain ShellcheckConfig
	var decoded plain
	if err := node.Load(&decoded, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if err := validateShellcheckBooleans(node, "extended-analysis", "external-sources"); err != nil {
		return err
	}
	var values map[string]yaml.Node
	if err := node.Decode(&values); err != nil {
		return err
	}
	for _, key := range []string{"disable", "enable", "source-path"} {
		value := values[key]
		for value.Kind == yaml.AliasNode {
			value = *value.Alias
		}
		for _, entry := range value.Content {
			for entry.Kind == yaml.AliasNode {
				entry = entry.Alias
			}
			if entry.Tag != "!!str" {
				return fmt.Errorf("tools.shellcheck.config.%s entries must be strings at line %d", key, entry.Line)
			}
		}
	}
	for _, code := range decoded.Disable {
		if !shellcheckCodeSelector.MatchString(code) {
			return fmt.Errorf("tools.shellcheck.config.disable: invalid code selector %q", code)
		}
	}
	for _, name := range decoded.Enable {
		if !shellcheckOptionalName.MatchString(name) {
			return fmt.Errorf("tools.shellcheck.config.enable: invalid check name %q", name)
		}
	}
	if decoded.Shell != nil && !slices.Contains([]string{"sh", "bash", "dash", "ksh", "busybox"}, *decoded.Shell) {
		return fmt.Errorf("tools.shellcheck.config.shell: unknown dialect %q", *decoded.Shell)
	}
	for _, path := range decoded.SourcePath {
		if _, err := shellcheckDirectivePath(path); err != nil {
			return err
		}
	}
	*config = ShellcheckConfig(decoded)
	return nil
}

func shellcheckDirectivePath(path string) (string, error) {
	if path == "" || strings.ContainsAny(path, "\r\n\x00") {
		return "", errors.New("tools.shellcheck.config.source-path: expected a nonempty, single-line path")
	}
	// ShellCheck's directive parser does not interpret escapes inside quotes.
	if !strings.Contains(path, `"`) {
		return `"` + path + `"`, nil
	}
	if !strings.Contains(path, "'") {
		return "'" + path + "'", nil
	}
	if !strings.ContainsAny(path, " \t") && path[0] != '\'' && path[0] != '"' {
		return path, nil
	}
	return "", fmt.Errorf("tools.shellcheck.config.source-path: ShellCheck cannot represent path %q containing whitespace and both quote types", path)
}

func (config *ShellcheckConfig) directives() (string, error) {
	if config == nil {
		return "", nil
	}
	var out strings.Builder
	write := func(key, value string) {
		out.WriteString("# shellcheck " + key + "=" + value + "\n")
	}
	if len(config.Disable) > 0 {
		write("disable", strings.Join(config.Disable, ","))
	}
	if len(config.Enable) > 0 {
		write("enable", strings.Join(config.Enable, ","))
	}
	if config.ExtendedAnalysis != nil {
		write("extended-analysis", strconv.FormatBool(*config.ExtendedAnalysis))
	}
	for _, path := range config.SourcePath {
		encoded, err := shellcheckDirectivePath(path)
		if err != nil {
			return "", err
		}
		write("source-path", encoded)
	}
	return out.String(), nil
}
