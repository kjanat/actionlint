package config

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
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
	Lint     *LintConfig `yaml:"lint" jsonschema:"nullable"`
	lintNode any
	lintSet  bool
	// Tools overlays external checker settings for the selected files.
	Tools     *ToolsConfig `yaml:"tools" jsonschema:"nullable"`
	toolsNode any
	toolsSet  bool
	// Repository overlays facts for matching files without changing rule selection.
	Repository     RepositoryConfig `yaml:"repository" jsonschema:"nullable"`
	repositoryNode any
	repositorySet  bool
	// LocalActions overlays generated action source mappings for matching files.
	LocalActions    LocalActionSources `yaml:"local-actions" jsonschema:"nullable"`
	localActionsSet bool
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
	next.lintNode, next.lintSet = object["lint"]
	next.toolsNode, next.toolsSet = object["tools"]
	next.repositoryNode, next.repositorySet = object["repository"]
	_, next.localActionsSet = object["local-actions"]
	*o = Override(next)
	return nil
}

// EncodeConfig preserves omitted fields in partial overlays during round trips.
func (o Override) EncodeConfig() (any, error) {
	value := map[string]any{"includes": o.Includes}
	if o.repositorySet {
		value["repository"] = o.repositoryNode
	}
	if o.localActionsSet || o.LocalActions != nil {
		value["local-actions"] = o.LocalActions
	}
	if o.Excludes != nil {
		value["excludes"] = o.Excludes
	}
	if o.lintSet {
		value["lint"] = o.lintNode
	} else if o.Lint != nil {
		selected, err := lintConfigValue(*o.Lint)
		if err != nil {
			return nil, err
		}
		value["lint"] = selected
	}
	if o.toolsSet {
		value["tools"] = o.toolsNode
	} else if o.Tools != nil {
		selected, err := programmaticToolsValue(o.Tools)
		if err != nil {
			return nil, err
		}
		value["tools"] = selected
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
	rules, err := binding.Encode(config.Rules)
	if err != nil {
		return nil, err
	}
	if config.Rules.Disable == nil {
		removeField(rules, "disable")
	}
	value := map[string]any{}
	if config.Dependencies.FailOnAll != nil || config.Dependencies.FailOnCategories != nil || config.Dependencies.FailOnRules != nil {
		value["dependencies"] = config.Dependencies
	}
	if config.MessageIgnores != nil {
		value["message-ignores"] = config.MessageIgnores
	}
	if config.Enabled != nil {
		value["enabled"] = config.Enabled
	}
	if len(members(rules)) != 0 {
		value["rules"] = rules
	}
	return binding.Encode(value)
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
	tools := config.Tools
	repository, err := binding.Encode(config.Repository)
	if err != nil {
		return nil, err
	}
	if repository == nil {
		repository = explicitNull{}
	}
	localActions := config.LocalActions
	for _, override := range config.Overrides {
		if !override.matches(path) {
			continue
		}
		if override.repositorySet {
			repository = mergeRepositoryNodes(repository, override.repositoryNode)
		}
		if override.localActionsSet || override.LocalActions != nil {
			if override.LocalActions == nil {
				localActions = nil
			} else {
				overlay, err := normalizedLocalActionSources(override.LocalActions)
				if err != nil {
					return nil, err
				}
				next := make(LocalActionSources)
				maps.Copy(next, localActions)
				maps.Copy(next, overlay)
				localActions = next
			}
		}
		if override.toolsSet || override.Tools != nil {
			tools, err = ruleSet.mergeToolsOverride(tools, override)
			if err != nil {
				return nil, err
			}
		}
		overlay := override.lintNode
		if !override.lintSet {
			if override.Lint == nil {
				continue
			}
			overlay, err = lintConfigValue(*override.Lint)
			if err != nil {
				return nil, err
			}
		}
		overlay, err = ruleSet.normalizeRuleLocations(overlay)
		if err != nil {
			return nil, err
		}
		value = configtree.Merge(ruleSet.expandLintShorthands(value), overlay)
	}
	next := *config
	if err := binding.Decode(rawValue(repository), &next.Repository); err != nil {
		return nil, err
	}
	next.Tools, next.LocalActions, next.Overrides = tools, localActions, nil
	next.Lint = LintConfig{}
	if err := binding.Decode(value, &next.Lint); err != nil {
		return nil, err
	}
	next.Lint.Rules.bindingSources = config.Lint.Rules.bindingSources
	next.Paths, next.Policy = nil, Policy{}
	ruleSet.projectSettings(&next)
	return &next, nil
}

func (ruleSet RuleSet) mergeToolsOverride(base ToolsConfig, override Override) (ToolsConfig, error) {
	encoded, err := binding.Encode(map[string]any{"tools": base})
	if err != nil {
		return nil, err
	}
	var value any
	if override.toolsSet {
		value = override.toolsNode
	} else {
		value, err = programmaticToolsValue(override.Tools)
		if err != nil {
			return nil, err
		}
	}
	merged := configtree.Merge(ruleSet.normalizeToolSwitch(encoded), ruleSet.normalizeToolSwitch(map[string]any{"tools": value}))
	if err := ruleSet.validateToolSettings(merged); err != nil {
		return nil, err
	}
	var decoded struct {
		Tools ToolsConfig `yaml:"tools"`
	}
	if err := binding.Decode(merged, &decoded); err != nil {
		return nil, err
	}
	for name, tool := range decoded.Tools {
		if tool.Config == nil {
			continue
		}
		source := base[name].Config
		if override.Tools != nil {
			if selected := (*override.Tools)[name].Config; selected != nil {
				source = selected
			}
		}
		if source != nil {
			tool.Config.fromInput = source.fromInput
		}
	}
	return decoded.Tools, nil
}

// Nil fields in programmatic settings are omitted. Parsed settings record resets separately.
func programmaticToolsValue(tools *ToolsConfig) (any, error) {
	value, err := binding.Encode(tools)
	if err != nil {
		return nil, err
	}
	var prune func(any)
	prune = func(value any) {
		if object, ok := value.(map[string]any); ok {
			for key, child := range object {
				if child == nil {
					delete(object, key)
				} else {
					prune(child)
				}
			}
		} else {
			for _, child := range elements(value) {
				prune(child)
			}
		}
	}
	prune(value)
	return value, nil
}

func (ruleSet RuleSet) expandLintShorthands(value any) any {
	for category := range (&LintRulesConfig{}).Groups() {
		value = expandRuleLevel(value, "rules", category)
		for _, descriptor := range ruleSet.Rules() {
			if descriptor.Group == category {
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
