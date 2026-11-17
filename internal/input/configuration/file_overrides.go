package configuration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/input/selection"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

// Override scopes settings to selected files. Other feature sections can
// share this scope when formatting and language-server support are introduced.
type Override struct {
	// Includes contains repository-relative file paths or doublestar globs.
	// At least one positive pattern is required. Prefix a pattern with ! to exclude
	// matches from this entry; exclusions win regardless of pattern order.
	Includes []string `yaml:"includes" jsonschema:"required,minItems=1"`
	// Excludes removes files from this override; exclusions always win.
	Excludes []string `yaml:"excludes,omitempty" jsonschema:"nullable"`
	// Lint overlays only explicitly supplied lint fields. Null resets lint defaults.
	Lint *LintConfig `yaml:"lint" jsonschema:"nullable"`
	// Tools overlays external checker settings for the selected files.
	Tools *ToolsConfig `yaml:"tools" jsonschema:"nullable"`
	// Repository overlays facts for matching files without changing rule selection.
	Repository RepositoryConfig `yaml:"repository" jsonschema:"nullable"`
	// LocalActions overlays generated action source mappings for matching files.
	LocalActions LocalActionSources `yaml:"local-actions" jsonschema:"nullable"`
	partial      *document.Node
}

// DecodeConfig validates paths and records which fields were omitted or reset.
func (o *Override) DecodeConfig(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("override must be a mapping")
	}
	type plain Override
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	positive := false
	for i, pattern := range next.Includes {
		if !selection.ValidPattern(pattern) {
			return binding.At("includes", binding.At(strconv.Itoa(i), fmt.Errorf("invalid repository-relative override glob %q", pattern)))
		}
		positive = positive || !strings.HasPrefix(pattern, "!")
	}
	for i, pattern := range next.Excludes {
		if !selection.ValidPattern(pattern) {
			return binding.At("excludes", binding.At(strconv.Itoa(i), fmt.Errorf("invalid override exclusion %q", pattern)))
		}
	}
	if !positive {
		return binding.At("includes", errors.New("overrides.includes requires at least one positive file glob"))
	}
	next.partial = document.Capture(object, nil)
	*o = Override(next)
	return nil
}

// CopyConfig copies typed settings and their captured partial independently.
func (o Override) CopyConfig() (any, error) {
	type plain Override
	copied, err := binding.DetachPublicFields(plain(o))
	if err != nil {
		return nil, err
	}
	next := copied
	if o.partial != nil {
		value, err := binding.DetachValue(o.partial.Value())
		if err != nil {
			return nil, err
		}
		next.partial = document.Capture(value, o.partial.Origins())
	}
	return Override(next), nil
}

// EncodeConfig preserves omitted fields in partial overlays during round trips.
func (o Override) EncodeConfig() (any, error) {
	var value any
	if o.partial != nil {
		value = o.partial.Value()
	} else {
		type plain Override
		var err error
		value, err = binding.EncodePartial(plain(o))
		if err != nil {
			return nil, err
		}
	}
	setField(value, "includes", o.Includes)
	if o.Excludes != nil {
		setField(value, "excludes", o.Excludes)
	} else {
		removeField(value, "excludes")
	}
	return value, nil
}

func (o Override) matches(path string) bool {
	return selection.MatchPatterns(o.Includes, o.Excludes, path)
}

func lintConfigValue(config LintConfig) (any, error) {
	if config.source != nil {
		return cloneValue(config.source), nil
	}
	type plain LintConfig
	value, err := binding.Encode(plain(config))
	if err != nil {
		return nil, err
	}
	rules := field(value, "rules")
	if config.Rules.Disable == nil {
		removeField(rules, "disable")
	}
	if len(members(rules)) == 0 {
		removeField(value, "rules")
	}
	return value, nil
}

// ForFile gives each checked file its own effective settings.
func (ruleSet RuleSet) ForFile(config *Config, path, root string) (*Config, error) {
	var err error
	config, err = ruleSet.capturedRuleConfig(config)
	if err != nil || config == nil {
		return config, err
	}
	if config.LocalActions != nil {
		next := *config
		next.LocalActions, err = normalizedLocalActionSources(config.LocalActions)
		if err != nil {
			return nil, err
		}
		config = &next
	}
	if len(config.Overrides) == 0 {
		return config, nil
	}
	path = RepositoryRelativePath(path, root)
	value, err := lintConfigValue(config.Lint)
	if err != nil {
		return nil, err
	}
	if config.Lint.source == nil {
		encoded, err := ruleSet.EncodeValue(config)
		if err != nil {
			return nil, err
		}
		if lint := field(encoded, "lint"); lint != nil {
			value = lint
		}
	}
	value, err = ruleSet.normalizeRuleLocations(value)
	if err != nil {
		return nil, err
	}

	encoded, err := ruleSet.EncodeValue(config)
	if err != nil {
		return nil, err
	}
	setField(encoded, "lint", ruleSet.expandLintShorthands(value))
	delete(members(encoded), "overrides")
	current := document.Capture(ruleSet.normalizeToolSwitch(encoded), config.Lint.locations)
	for _, override := range config.Overrides {
		if !override.matches(path) {
			continue
		}
		patch, err := override.EncodeConfig()
		if err != nil {
			return nil, err
		}
		delete(members(patch), "includes")
		delete(members(patch), "excludes")
		if local := field(patch, "local-actions"); local != nil {
			normalizeLocalActionSourceNode(local)
		}
		if lint := field(patch, "lint"); lint != nil {
			lint, err = ruleSet.normalizeRuleLocations(rawValue(lint))
			if err != nil {
				return nil, err
			}
			setField(patch, "lint", ruleSet.expandLintShorthands(lint))
		}
		var origins map[string]document.Location
		if override.partial != nil {
			origins = override.partial.Origins()
		}
		current = document.Merge(current, document.Capture(ruleSet.normalizeToolSwitch(patch), origins), MergePlan())
	}
	merged := current.Value()
	if err := ruleSet.validateToolSettings(merged); err != nil {
		return nil, err
	}
	var next Config
	if err := ruleSet.DecodeValue(merged, &next); err != nil {
		return nil, err
	}
	origins := current.Origins()
	next.Tools.setInputOrigins(origins, "/tools")
	if next.LocalActions != nil {
		next.LocalActions, err = normalizedLocalActionSources(next.LocalActions)
		if err != nil {
			return nil, err
		}
	}
	next.Overrides = nil
	next.Lint.locations = origins
	next.Lint.Rules.bindingSources = config.Lint.Rules.bindingSources
	next.Paths, next.Policy = nil, Policy{}
	ruleSet.projectSettings(&next)
	return &next, nil
}

func (ruleSet RuleSet) expandLintShorthands(value any) any {
	for category := range (&LintRulesConfig{}).Groups() {
		value = expandRuleLevel(value, "rules", category)
		for _, descriptor := range ruleSet.Rules() {
			if descriptor.Category == category {
				value = expandRuleLevel(value, "rules", category, descriptor.Name)
			}
		}
	}
	return value
}

func expandRuleLevel(value any, path ...string) any {
	if len(path) == 0 {
		if isBool(value) || text(value) != "" && text(value) != "default" {
			return map[string]any{"level": value}
		}
		return value
	}
	if !isMapping(value) {
		return value
	}
	expanded := cloneMapping(value)
	if selected := field(value, path[0]); selected != nil {
		setField(expanded, path[0], expandRuleLevel(rawValue(selected), path[1:]...))
	}
	return expanded
}
