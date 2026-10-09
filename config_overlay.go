package actionlint

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"actionlint.kjanat.dev/internal/configtree"
	"go.yaml.in/yaml/v4"
)

// ConfigKeys returns the supported top-level configuration keys in declaration order.
// Overlay documents use these names and YAML types.
func ConfigKeys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		if name := f.Tag.Get("yaml"); name != "" && name != "-" {
			keys = append(keys, strings.Split(name, ",")[0])
		}
	}
	return keys
}

// ConfigOverlay is a validated, immutable configuration override. Construct it
// with ParseConfigOverlay; the original YAML nodes preserve null, false and [].
type ConfigOverlay struct {
	name string
	node *yaml.Node
}

// ConfigOverlayError reports individually valid inputs whose combined settings
// are invalid, for example timeout bounds that conflict with the config file.
type ConfigOverlayError struct {
	inputs []string
	cause  error
}

func (e *ConfigOverlayError) Error() string {
	return fmt.Sprintf("configuration after inputs %s: %v", strings.Join(e.inputs, ", "), e.cause)
}

// Unwrap returns the configuration validation error.
func (e *ConfigOverlayError) Unwrap() error { return e.cause }

// ParseConfigOverlay parses a complete YAML/JSON document (input "config") or
// the YAML/JSON value of one ConfigKeys entry. Unknown keys are rejected here;
// existing config files retain their parsing behavior.
func ParseConfigOverlay(input string, content []byte) (ConfigOverlay, error) {
	var doc yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(content))
	if err := d.Decode(&doc); err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if len(doc.Content) != 1 {
		return ConfigOverlay{}, fmt.Errorf("input %s: expected one YAML or JSON value", input)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return ConfigOverlay{}, fmt.Errorf("input %s: expected one YAML or JSON document", input)
	}
	node := doc.Content[0]
	if input != "config" {
		known := false
		for _, key := range ConfigKeys() {
			known = known || key == input
		}
		if !known {
			return ConfigOverlay{}, fmt.Errorf("unknown configuration input %q", input)
		}
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: input}, node,
		}}
	}
	var config Config
	if err := node.Load(&config, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if len(config.Extends) > 0 {
		return ConfigOverlay{}, fmt.Errorf("input %s: extends requires a config file origin", input)
	}
	if _, err := resolveConfigNode(node, nil); err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	node, err := configtree.Expand(node, make(map[*yaml.Node]bool))
	if err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	return ConfigOverlay{input, normalizeToolSwitch(node)}, nil
}

type loadedConfig struct {
	resolvedConfig
	filename string
}

// ConfigReport describes the configuration actually selected for one project.
type ConfigReport struct {
	Project    string
	File       string
	Explicit   bool
	Overrides  []string
	Inspection ConfigInspection
}

type analysisConfigState struct {
	sync.Mutex
	source   *loadedConfig
	overlays []ConfigOverlay
	onLoaded func(ConfigReport)
	loaded   map[*Project]*Config
	reports  map[*Project]ConfigReport
}

func (a *AnalysisSession) configForProject(project *Project) (*Config, error) {
	var cfg *Config
	var source *loadedConfig
	if a.defaultConfig != nil {
		cfg = a.defaultConfig
		if a.configState != nil {
			source = a.configState.source
		}
	} else if project != nil {
		cfg, source = project.Config(), project.config
	}
	s := a.configState
	if s == nil {
		return cfg, nil
	}
	s.Lock()
	var notification *ConfigReport
	defer func() {
		s.Unlock()
		if notification != nil && s.onLoaded != nil {
			s.onLoaded(*notification)
		}
	}()
	if loaded, ok := s.loaded[project]; ok {
		return loaded, nil
	}
	report := ConfigReport{Explicit: a.defaultConfig != nil}
	var node *yaml.Node
	var resolved resolvedConfig
	if project != nil {
		report.Project = project.RootDir()
	}
	if source != nil {
		node, report.File = source.node, source.filename
		resolved = source.resolvedConfig
	}
	inputs := make(map[*yaml.Node]configInput)
	if len(s.overlays) > 0 {
		expanded, err := configtree.Expand(node, make(map[*yaml.Node]bool))
		if err != nil {
			return nil, fmt.Errorf("configuration %s: %w", report.File, err)
		}
		node = normalizeToolSwitch(expanded)
	}
	for _, overlay := range s.overlays {
		markConfigInput(overlay.node, overlay.name, inputs)
		node = configtree.Merge(node, overlay.node, inputs)
		report.Overrides = append(report.Overrides, overlay.name)
	}
	if len(s.overlays) > 0 {
		var err error
		resolved, err = resolveConfigNode(node, inputs)
		if err != nil {
			return nil, &ConfigOverlayError{report.Overrides, err}
		}
		cfg = resolved.config
		if source != nil {
			cfg.filename = absPath(source.filename)
			cfg.configFiles = source.config.configFiles
		}
	} else if source == nil {
		var err error
		resolved, err = resolveConfigNode(nil, nil)
		if err != nil {
			return nil, err
		}
	}
	report.Inspection = ConfigInspection{Path: report.File, Config: resolved.values, Origins: resolved.origins, Warnings: resolved.warnings}
	if s.onLoaded == nil {
		for _, warning := range resolved.warnings {
			_, _ = fmt.Fprintf(a.logOut, "%s:%d:%d: warning: %s\n", report.File, warning.Line, warning.Column, warning.Message)
		}
	}
	s.loaded[project] = cfg
	s.reports[project] = report
	notification = &report
	return cfg, nil
}

type configInput = configtree.Input

func markConfigInput(node *yaml.Node, input string, inputs map[*yaml.Node]configInput) {
	inputs[node] = configInput{Name: input}
	for _, child := range node.Content {
		markConfigInput(child, input, inputs)
	}
}
