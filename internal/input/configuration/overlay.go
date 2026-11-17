package configuration

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
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
// node holds the settings normalized by the rule set that parsed the overlay.
// lint holds the lint settings before rule locations are normalized: a parse
// before plugins load cannot place plugin rules, so Overlaid normalizes them
// with the rule set that resolves the run's configuration.
type Overlay struct {
	name   string
	node   any
	lint   any
	inputs map[string]document.Location
}

// ParseOverlay parses a complete YAML/JSON document (input "config") or
// the YAML/JSON value of one ConfigKeys entry. It rejects unknown keys.
func (ruleSet RuleSet) ParseOverlay(input string, content []byte, additional ...string) (Overlay, error) {
	return ruleSet.parseOverlay(input, content, true, additional...)
}

// ParseOverlayBeforePlugins validates input structure and settings before plugin
// rule names are available. Overlaid resolves its rule settings with the
// final rule set after the selected plugins have loaded.
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
	lint := cloneValue(field(node, "lint"))
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

	return Overlay{name: input, node: ruleSet.normalizeToolSwitch(node), lint: lint, inputs: inputs}, nil
}

// ResolveBeforePlugins merges already parsed overlays for dependency selection.
// Rule-name binding is deferred to Overlaid, which the analysis session runs once.
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
		return ResolvedConfig{}, configreport.NewOverlayError(names, err)
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
	return Overlay{name: o.name, node: node, lint: cloneValue(o.lint), inputs: maps.Clone(o.inputs)}, nil
}

// settingsFor returns the overlay's settings with its lint settings
// normalized by ruleSet.
func (o Overlay) settingsFor(ruleSet RuleSet) (any, error) {
	if o.lint == nil {
		return o.node, nil
	}
	lint, err := ruleSet.normalizeRuleLint(map[string]any{"lint": cloneValue(o.lint)})
	if err != nil {
		return nil, err
	}
	node := cloneValue(o.node)
	setField(node, "lint", field(lint, "lint"))
	return node, nil
}

// Overlaid merges parsed overlays over a loaded source and resolves the
// result once with ruleSet, which must be the rule set of the analysis that
// reads it, so the result needs no further binding. A configuration supplied
// without its source contributes its lint settings to the merge. Without
// overlays it returns the supplied configuration and the source's document.
func (ruleSet RuleSet) Overlaid(cfg *Config, source *LoadedFile, overlays []Overlay, additional ...string) (*Config, ResolvedConfig, error) {
	var node any
	var resolved ResolvedConfig
	var bindingSources []bindingSource
	if cfg != nil {
		bindingSources = slices.Clone(cfg.Lint.Rules.bindingSources)
	}
	if source != nil {
		node, resolved = source.Value, source.ResolvedConfig
	} else if cfg != nil && len(overlays) > 0 {
		encoded, err := ruleSet.EncodeValue(cfg)
		if err != nil {
			return nil, ResolvedConfig{}, err
		}
		if lint := field(encoded, "lint"); lint != nil {
			node, err = ruleSet.normalizeRuleLint(map[string]any{"lint": lint})
			if err != nil {
				return nil, ResolvedConfig{}, err
			}
		}
	}
	inputs := maps.Clone(resolved.Inputs)
	if inputs == nil {
		inputs = make(map[string]document.Location)
	}
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		settings, err := overlay.settingsFor(ruleSet)
		if err != nil {
			return nil, ResolvedConfig{}, configreport.NewOverlayError([]string{overlay.name}, err)
		}
		var decoded Config
		if err := ruleSet.DecodeValue(settings, &decoded); err != nil {
			return nil, ResolvedConfig{}, err
		}
		bindingSources = append(bindingSources, ruleBindingSources(overlay.name, &decoded, overlay.inputs)...)
		merged := document.Merge(document.Capture(node, inputs), document.Capture(settings, overlay.inputs), MergePlan())
		node, inputs = merged.Value(), merged.Origins()
		names = append(names, overlay.name)
	}
	if len(overlays) > 0 {
		var err error
		resolved, err = ruleSet.ResolveValue(node, inputs, additional...)
		if err != nil {
			return nil, ResolvedConfig{}, configreport.NewOverlayError(names, err)
		}
		cfg = resolved.Config
		cfg.Lint.Rules.bindingSources = bindingSources
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
