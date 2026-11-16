package configuration

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/input/configuration/report"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tools"
)

// Keys returns the supported top-level configuration keys in declaration order.
// Overlay documents use these names and YAML types.
func Keys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		if name := f.Tag.Get("yaml"); name != "" && name != "-" {
			keys = append(keys, strings.Split(name, ",")[0])
		}
	}
	return keys
}

// Overlay is a validated, immutable configuration override. Construct it
// with ParseOverlay; the original YAML nodes preserve null, false and [].
type Overlay struct {
	name   string
	node   any
	rules  any
	inputs map[string]document.Location
}

// ParseOverlay parses a complete YAML/JSON document (input "config") or
// the YAML/JSON value of one ConfigKeys entry. It rejects unknown keys.
func (ruleSet RuleSet) ParseOverlay(input string, content []byte, additional ...string) (Overlay, error) {
	return ruleSet.parseOverlay(input, content, true, additional...)
}

// ParseOverlayBeforePlugins validates input structure and settings before plugin
// rule names are available. Normal configuration resolution binds those names
// after the selected plugins have loaded.
func (ruleSet RuleSet) ParseOverlayBeforePlugins(input string, content []byte) (Overlay, error) {
	return ruleSet.parseOverlay(input, content, false)
}

func (ruleSet RuleSet) parseOverlay(input string, content []byte, bindRules bool, additional ...string) (Overlay, error) {
	if len(strings.TrimSpace(string(content))) == 0 {
		return Overlay{}, fmt.Errorf("input %s: expected a configuration value", input)
	}
	parsed, err := document.ParseConfigDocument("actionlint.yaml", content)
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	node := parsed.Value()
	inputs := parsed.Origins()
	for path, location := range inputs {
		location.Name, location.File = input, ""
		inputs[path] = location
	}
	if input != "config" {
		known := false
		for _, key := range append(Keys(), legacyKeys...) {
			known = known || key == input
		}
		if !known {
			return Overlay{}, fmt.Errorf("unknown configuration input %q", input)
		}
		node = map[string]any{input: node}
		prefixed := make(map[string]document.Location, len(inputs)+1)
		prefixed[""] = inputs[""]
		for path, location := range inputs {
			prefixed[document.Path("", input)+path] = location
		}
		inputs = prefixed
	}
	if warnings := configWarnings(node, nil); len(warnings) != 0 {
		return Overlay{}, fmt.Errorf("input %s: %s", input, warnings[0].Message)
	}
	inputs = ruleSet.migrationInputs(node, inputs)
	node, err = ruleSet.NormalizeForMerge(node)
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, sourceError(err, inputs))
	}
	var config Config
	if err := ruleSet.decodeValue(node, &config, bindRules); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, sourceError(err, inputs))
	}
	if len(config.Extends) > 0 {
		return Overlay{}, fmt.Errorf("input %s: extends requires a config file origin", input)
	}
	if _, err := ruleSet.resolveValue(node, inputs, bindRules, additional...); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	rules := captureRuleDocument(node)
	if local := field(node, "local-actions"); local != nil {
		normalizeLocalActionSourceNode(local)
	}
	if lint := field(node, "lint"); lint != nil {
		normalized, err := ruleSet.normalizeRuleLocations(lint)
		if err != nil {
			return Overlay{}, fmt.Errorf("input %s: %w", input, err)
		}
		setField(node, "lint", normalized)
	}

	return Overlay{name: input, node: ruleSet.normalizeToolSwitch(node), rules: rules, inputs: inputs}, nil
}

// ResolveBeforePlugins merges already parsed overlays for dependency selection.
// Only rule-name binding is deferred; it is repeated by the analysis session.
func (ruleSet RuleSet) ResolveBeforePlugins(node any, overlays []Overlay) (ResolvedConfig, error) {
	inputs := make(map[string]document.Location)
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		merged := document.Merge(document.Capture(node, inputs), document.Capture(overlay.node, overlay.inputs), MergePlan())
		node, inputs = merged.Value(), merged.Origins()
		names = append(names, overlay.name)
	}
	resolved, err := ruleSet.resolveValue(node, inputs, false)
	if err != nil && len(names) != 0 {
		return ResolvedConfig{}, report.NewOverlayError(names, err)
	}
	return resolved, err
}

// LoadedFile is a ResolvedConfig together with the configuration file it came from.
type LoadedFile struct {
	ResolvedConfig
	Filename string
}

// Name returns the input that supplied this overlay.
func (o Overlay) Name() string { return o.name }

// BindPaths returns a copy whose relative tool paths resolve from directory.
func (o Overlay) BindPaths(directory string) (Overlay, error) {
	node := cloneValue(o.node)
	tools.NormalizePaths(node, directory)
	rules := captureRuleDocument(o.rules)
	tools.NormalizePaths(rules, directory)
	return Overlay{name: o.name, node: node, rules: rules, inputs: maps.Clone(o.inputs)}, nil
}

// Overlaid merges parsed overlays over a loaded source and resolves the result.
// Without overlays it returns the supplied configuration and the source's document.
func (ruleSet RuleSet) Overlaid(cfg *Config, source *LoadedFile, overlays []Overlay, additional ...string) (*Config, ResolvedConfig, error) {
	var node any
	var resolved ResolvedConfig
	var bindingSources []bindingSource
	captured := &ruleSources{}
	if cfg != nil {
		bindingSources = slices.Clone(cfg.Lint.Rules.bindingSources)
		if cfg.Lint.Rules.sources != nil {
			captured = cfg.Lint.Rules.sources
		} else {
			var err error
			baseline, err := ruleSet.EncodeValue(cfg)
			if err != nil {
				return nil, ResolvedConfig{}, err
			}
			captured.base = captureRuleDocument(baseline)
		}
	}
	if source != nil {
		node, resolved = source.Value, source.ResolvedConfig
	}
	inputs := maps.Clone(resolved.Inputs)
	if inputs == nil {
		inputs = make(map[string]document.Location)
	}
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		captured = captured.withOverlay(overlay.rules)
		var settings Config
		if err := ruleSet.DecodeValue(overlay.node, &settings); err != nil {
			return nil, ResolvedConfig{}, err
		}
		bindingSources = append(bindingSources, ruleBindingSources(overlay.name, &settings, overlay.inputs)...)
		merged := document.Merge(document.Capture(node, inputs), document.Capture(overlay.node, overlay.inputs), MergePlan())
		node, inputs = merged.Value(), merged.Origins()
		names = append(names, overlay.name)
	}
	if len(overlays) > 0 {
		var err error
		resolved, err = ruleSet.ResolveValue(node, inputs, additional...)
		if err != nil {
			return nil, ResolvedConfig{}, report.NewOverlayError(names, err)
		}
		cfg = resolved.Config
		cfg.Lint.Rules.bindingSources = bindingSources
		cfg.Lint.Rules.sources = captured
		if source != nil {
			resolved.Provenance = source.Provenance
			for i := range resolved.Warnings {
				if resolved.Warnings[i].File == "" {
					resolved.Warnings[i].File = source.Provenance.Path()
				}
			}
			resolved.Warnings = MergeWarnings(source.Warnings, resolved.Warnings)
		}
	} else if source == nil {
		var err error
		resolved, err = ruleSet.ResolveValue(nil, nil, additional...)
		if err != nil {
			return nil, ResolvedConfig{}, err
		}
	}
	return cfg, resolved, nil
}
