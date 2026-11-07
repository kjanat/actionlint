package config

import "github.com/kjanat/go-yaml/v4"

// normalizeRuleLocations gives named settings one merge key across accepted
// category spellings. Group defaults stay in their declared groups because
// their membership can depend on plugin declarations loaded later.
func (ruleSet RuleSet) normalizeRuleLocations(lint *yaml.Node) (*yaml.Node, error) {
	rules := field(lint, "rules")
	if rules == nil || rules.Kind != yaml.MappingNode {
		return lint, nil
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
		var setting *yaml.Node
		level := Level("default")
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
		// Options come from the last named setting. A default level defers
		// to earlier explicit levels, as SettingFor does within one layer.
		ownLevel, err := settingLevelNode(setting)
		if err != nil {
			return nil, err
		}
		if ownLevel == "default" && level != "default" {
			if setting.Kind == yaml.MappingNode {
				setting = cloneMapping(setting)
			} else {
				setting = mapping()
			}
			setField(setting, "level", textNode(string(level)))
		}
		names := []string{descriptor.Name}
		for _, alias := range descriptor.Aliases {
			names = append(names, alias.Name)
		}
		for _, category := range append(categories, descriptor.Category) {
			group := field(nextRules, category)
			if group == nil || group.Kind != yaml.MappingNode {
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

func (ruleSet RuleSet) ruleSettingNode(group *yaml.Node, name string) *yaml.Node {
	for i := 0; group != nil && group.Kind == yaml.MappingNode && i+1 < len(group.Content); i += 2 {
		if ruleSet.ResolveAlias(group.Content[i].Value) == name {
			return group.Content[i+1]
		}
	}
	return nil
}

func settingLevelNode(setting *yaml.Node) (Level, error) {
	if setting.Kind == yaml.MappingNode {
		setting = field(setting, "level")
	}
	if setting == nil || setting.ShortTag() == "!!null" {
		return "default", nil
	}
	var level Level
	err := setting.Decode(&level)
	return level, err
}
