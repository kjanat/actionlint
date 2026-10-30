package tools

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kjanat/go-yaml/v4"
)

// ShellcheckSettings overrides ShellCheck's workflow-derived defaults.
// A nil Config inherits the project selection; absent project config disables rc discovery.
type ShellcheckSettings struct {
	Config          ShellcheckConfigSelection
	Shell           string
	Exclude         []string
	ExternalSources *bool
}

// ShellcheckConfigSelection selects an rc file, discovery, or disabled rc loading.
type ShellcheckConfigSelection interface{ shellcheckConfigSelection() }

// ShellcheckRCFile selects an explicit rc file.
// Relative application paths use the process working directory. Path
// interpolations use the analyzed project's configuration and action context.
type ShellcheckRCFile string

func (ShellcheckRCFile) shellcheckConfigSelection() {}

// ShellcheckRCMode selects discovery or disables rc loading.
type ShellcheckRCMode bool

func (ShellcheckRCMode) shellcheckConfigSelection() {}

const (
	// ShellcheckRCDisabled disables rc loading, including project-selected paths.
	ShellcheckRCDisabled ShellcheckRCMode = false
	// ShellcheckRCDiscover discovers configuration from the analysis working directory.
	ShellcheckRCDiscover ShellcheckRCMode = true
)

var (
	shellcheckCodeSelector = regexp.MustCompile(`^(all|(SC)?[0-9]+(-(SC)?[0-9]+)?)$`)
	shellcheckOptionalName = regexp.MustCompile(`^[a-zA-Z-]+$`)
)

// ShellcheckConfig is a typed representation of project-wide ShellCheck directives.
type ShellcheckConfig struct {
	// Disable lists codes, code ranges or all, for example [SC2086, SC3000-SC4000].
	Disable []string `yaml:"disable" jsonschema:"nullable,pattern=^(all|(SC)?[0-9]+(-(SC)?[0-9]+)?)$"`
	// Enable selects optional check names. Scalar all or true expands to [all].
	// This selection does not change the tool enabled setting.
	Enable []string `yaml:"enable" jsonschema:"nullable,pattern=^[a-zA-Z-]+$"`
	// Shell overrides the inferred dialect of scripts selected for ShellCheck.
	Shell *string `yaml:"shell" jsonschema:"nullable,enum=sh,enum=bash,enum=dash,enum=ksh,enum=busybox"`
	// ExtendedAnalysis enables or disables ShellCheck dataflow analysis. Omission keeps its default.
	ExtendedAnalysis *bool `yaml:"extended-analysis" jsonschema:"nullable"`
	// ExternalSources controls following sourced files. Omission preserves actionlint's enabled default.
	ExternalSources *bool `yaml:"external-sources" jsonschema:"nullable"`
	// SourcePath lists directories searched for sourced files, relative to the run step's effective working directory.
	// Scripts arrive on stdin, so SCRIPTDIR does not denote the workflow file's directory.
	SourcePath []string `yaml:"source-path" jsonschema:"nullable,minLength=1"`
}

// ParseShellcheckConfig decodes and validates inline tools.shellcheck.config
// settings, as configuration decoding delivers them.
func ParseShellcheckConfig(values map[string]any) (*ShellcheckConfig, error) {
	var node yaml.Node
	if err := node.Encode(values); err != nil {
		return nil, fmt.Errorf("tools.shellcheck.config: %w", err)
	}
	var config ShellcheckConfig
	if err := node.Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// Values validates the settings and returns them in configuration form. Unset
// settings are omitted.
func (config *ShellcheckConfig) Values() (map[string]any, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	values := map[string]any{}
	for key, list := range map[string][]string{"disable": config.Disable, "enable": config.Enable, "source-path": config.SourcePath} {
		if list != nil {
			entries := make([]any, len(list))
			for i, entry := range list {
				entries[i] = entry
			}
			values[key] = entries
		}
	}
	if config.Shell != nil {
		values["shell"] = *config.Shell
	}
	for key, enabled := range map[string]*bool{"extended-analysis": config.ExtendedAnalysis, "external-sources": config.ExternalSources} {
		if enabled != nil {
			values[key] = *enabled
		}
	}
	return values, nil
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

// UnmarshalYAML validates native directive values before analysis starts.
func (config *ShellcheckConfig) UnmarshalYAML(node *yaml.Node) error {
	var values map[string]yaml.Node
	if err := node.Decode(&values); err != nil {
		return err
	}
	if value, ok := values["enable"]; ok {
		for value.Kind == yaml.AliasNode {
			value = *value.Alias
		}
		if value.Kind == yaml.ScalarNode && (value.Tag == "!!str" && value.Value == "all" || value.Tag == "!!bool" && strings.EqualFold(value.Value, "true")) {
			value.Kind, value.Tag, value.Value = yaml.SequenceNode, "!!seq", ""
			value.Content = []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "all"}}
			values["enable"] = value
			normalized := *node
			normalized.Content = nil
			for _, key := range slices.Sorted(maps.Keys(values)) {
				entry := values[key]
				normalized.Content = append(normalized.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key, Line: entry.Line, Column: entry.Column}, &entry)
			}
			node = &normalized
		}
	}
	type plain ShellcheckConfig
	var decoded plain
	if err := node.Load(&decoded, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if err := validateShellcheckBooleans(node, "extended-analysis", "external-sources"); err != nil {
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
	validated := ShellcheckConfig(decoded)
	if err := validated.Validate(); err != nil {
		return err
	}
	*config = validated
	return nil
}

// Validate reports the first setting ShellCheck cannot accept as a directive.
func (config *ShellcheckConfig) Validate() error {
	for _, code := range config.Disable {
		if !shellcheckCodeSelector.MatchString(code) {
			return fmt.Errorf("tools.shellcheck.config.disable: invalid code selector %q", code)
		}
	}
	for _, name := range config.Enable {
		if !shellcheckOptionalName.MatchString(name) {
			return fmt.Errorf("tools.shellcheck.config.enable: invalid check name %q", name)
		}
	}
	if config.Shell != nil && !slices.Contains([]string{"sh", "bash", "dash", "ksh", "busybox"}, *config.Shell) {
		return fmt.Errorf("tools.shellcheck.config.shell: unknown dialect %q", *config.Shell)
	}
	for _, path := range config.SourcePath {
		if _, err := shellcheckDirectivePath(path); err != nil {
			return err
		}
	}
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

// Directives renders the settings as ShellCheck directive comments.
func (config *ShellcheckConfig) Directives() (string, error) {
	if config == nil {
		return "", nil
	}
	var out strings.Builder
	write := func(key, value string) {
		out.WriteString("# shellcheck ")
		out.WriteString(key)
		out.WriteByte('=')
		out.WriteString(value)
		out.WriteByte('\n')
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
