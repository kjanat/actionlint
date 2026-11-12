package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
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
	inputs map[string]Input
}

// OverlayError reports individually valid inputs whose combined settings
// are invalid, for example timeout bounds that conflict with the config file.
type OverlayError = configreport.OverlayError

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
	parsed, err := document.Parse("actionlint.yaml", content)
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	node := parsed.Value
	inputs := documentInputs(parsed)
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
		prefixed := make(map[string]Input, len(inputs)+1)
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
func (ruleSet RuleSet) ResolveBeforePlugins(node any, overlays []Overlay) (Resolved, error) {
	inputs := make(map[string]Input)
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		markOverlay(node, overlay.node, overlay.name, inputs, overlay.inputs)
		node = MergeDocuments(node, overlay.node)
		names = append(names, overlay.name)
	}
	resolved, err := ruleSet.resolveValue(node, inputs, false)
	if err != nil && len(names) != 0 {
		return Resolved{}, configreport.NewOverlayError(names, err)
	}
	return resolved, err
}

type Loaded struct {
	Resolved
	Filename string
}

type Input = configtree.Input

func MarkInput(value any, name string, inputs map[string]Input) {
	markOverlay(nil, value, name, inputs)
}

func markOverlay(base, overlay any, name string, inputs map[string]Input, locations ...map[string]Input) {
	var supplied map[string]Input
	if len(locations) > 0 {
		supplied = locations[0]
	}
	var mark func(any, bool, any, string)
	mark = func(base any, basePresent bool, value any, path string) {
		previous, hasSource := inputs[path]
		selected := supplied[path]
		if _, declared := supplied[path]; declared || supplied == nil {
			selected.Name = name
		}
		if isMapping(value) {
			if basePresent && base == nil && hasSource {
				reset := previous
				selected.Reset = &reset
			} else {
				selected.Reset = previous.Reset
			}
		} else {
			for key := range inputs {
				if strings.HasPrefix(key, path+"/") {
					delete(inputs, key)
				}
			}
		}
		inputs[path] = selected
		for key, child := range members(value) {
			prior, found := members(base)[key]
			mark(prior, found, child, document.Path(path, key))
		}
		for i, child := range elements(value) {
			mark(nil, false, child, document.Index(path, i))
		}
	}
	_, found := inputs[""]
	mark(base, found, overlay, "")
}

// Name returns the input that supplied this overlay.
func (o Overlay) Name() string { return o.name }

// BindPaths returns a copy whose relative tool paths resolve from directory.
func (o Overlay) BindPaths(directory string) (Overlay, error) {
	node := cloneValue(o.node)
	toolconfig.NormalizePaths(node, directory)
	rules := captureRuleDocument(o.rules)
	toolconfig.NormalizePaths(rules, directory)
	return Overlay{name: o.name, node: node, rules: rules, inputs: maps.Clone(o.inputs)}, nil
}

// Overlaid merges parsed overlays over a loaded source and resolves the result.
// Without overlays it returns the supplied configuration and the source's document.
func (ruleSet RuleSet) Overlaid(cfg *Config, source *Loaded, overlays []Overlay, additional ...string) (*Config, Resolved, error) {
	var node any
	var resolved Resolved
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
				return nil, Resolved{}, err
			}
			captured.base = captureRuleDocument(baseline)
		}
	}
	if source != nil {
		node, resolved = source.Value, source.Resolved
	}
	inputs := maps.Clone(resolved.Inputs)
	if inputs == nil {
		inputs = make(map[string]Input)
	}
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		captured = captured.withOverlay(overlay.rules)
		var settings Config
		if err := ruleSet.DecodeValue(overlay.node, &settings); err != nil {
			return nil, Resolved{}, err
		}
		bindingSources = append(bindingSources, ruleBindingSources(overlay.name, &settings, overlay.inputs)...)
		markOverlay(node, overlay.node, overlay.name, inputs, overlay.inputs)
		node = MergeDocuments(node, overlay.node)
		names = append(names, overlay.name)
	}
	if len(overlays) > 0 {
		var err error
		resolved, err = ruleSet.ResolveValue(node, inputs, additional...)
		if err != nil {
			return nil, Resolved{}, configreport.NewOverlayError(names, err)
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
			return nil, Resolved{}, err
		}
	}
	return cfg, resolved, nil
}
