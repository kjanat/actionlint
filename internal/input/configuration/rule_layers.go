package configuration

import (
	"maps"

	"actionlint.kjanat.dev/v2/internal/input/configuration/ruleconfig"
)

// Group membership can depend on plugin declarations that load later.
func (ruleSet RuleSet) normalizeRuleLocations(lint any) (any, error) {
	rules := field(lint, "rules")
	if !isMapping(rules) {
		return lint, nil
	}
	for category, group := range members(rules) {
		if err := validateRuleAliases(ruleSet, category, maps.All(members(group))); err != nil {
			return nil, err
		}
	}
	nextRules := cloneMapping(rules)
	changed := false
	for _, descriptor := range ruleSet.Rules() {
		legacyCategory := ruleSet.LegacyGroup(descriptor.Name)
		categories := []string{}
		if legacyCategory != "" && legacyCategory != descriptor.Category {
			categories = append(categories, legacyCategory)
		}
		if ruleSet.registered[descriptor.Name] {
			categories = append(categories, "plugins")
		}
		if len(categories) == 0 {
			continue
		}
		var setting any
		level := ruleconfig.Level("default")
		for _, category := range append(categories, descriptor.Category) {
			candidate := ruleSet.ruleSettingNode(field(nextRules, category), descriptor.Name)
			if candidate == nil {
				continue
			}
			candidateLevel, err := settingLevelNode(candidate)
			if err != nil {
				return nil, err
			}
			if candidateLevel != "default" {
				level = candidateLevel
			}
			setting = candidate
		}
		if setting == nil {
			continue
		}
		// Options come from the last named setting, and a default level defers to earlier explicit levels.
		declaredLevel, err := settingLevelNode(setting)
		if err != nil {
			return nil, err
		}
		if declaredLevel == "default" && level != "default" {
			if isMapping(setting) {
				setting = cloneMapping(setting)
			} else {
				setting = mapping()
			}
			setField(setting, "level", string(level))
		}
		names := []string{descriptor.Name}
		for _, alias := range descriptor.Aliases {
			names = append(names, alias.Name)
		}
		for _, category := range append(categories, descriptor.Category) {
			group := field(nextRules, category)
			if !isMapping(group) {
				continue
			}
			group = cloneMapping(group)
			for _, name := range names {
				removeField(group, name)
			}
			setField(nextRules, category, group)
		}
		targetNode, err := migrationGroup(field(nextRules, descriptor.Category))
		if err != nil {
			return nil, err
		}
		setField(targetNode, descriptor.Name, setting)
		setField(nextRules, descriptor.Category, targetNode)
		changed = true
	}
	if !changed {
		return lint, nil
	}
	next := cloneMapping(lint)
	setField(next, "rules", nextRules)
	return next, nil
}

func (ruleSet RuleSet) ruleSettingNode(group any, name string) any {
	for key := range members(group) {
		if ruleSet.ResolveAlias(key) == name {
			return field(group, key)
		}
	}
	return nil
}

func settingLevelNode(setting any) (ruleconfig.Level, error) {
	if isMapping(setting) {
		setting = field(setting, "level")
	}
	if setting == nil || isNull(setting) {
		return "default", nil
	}
	var level ruleconfig.Level
	err := decodeValue(setting, &level)
	return level, err
}

func (ruleSet RuleSet) normalizeRuleLint(value any) (any, error) {
	if !isMapping(value) {
		return value, nil
	}
	next := cloneMapping(value)
	if lint := field(value, "lint"); lint != nil {
		lint, err := ruleSet.normalizeRuleLocations(lint)
		if err != nil {
			return nil, err
		}
		setField(next, "lint", ruleSet.expandLintShorthands(lint))
	}
	return next, nil
}
