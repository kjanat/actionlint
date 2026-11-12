package config

// normalizeRuleLocations gives named settings one merge key across accepted
// category spellings. Group defaults stay in their declared groups because
// their membership can depend on plugin declarations loaded later.
func (ruleSet RuleSet) normalizeRuleLocations(lint any) (any, error) {
	rules := field(lint, "rules")
	if !isMapping(rules) {
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
		var setting any
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

func settingLevelNode(setting any) (Level, error) {
	if isMapping(setting) {
		setting = field(setting, "level")
	}
	if setting == nil || isNull(setting) {
		return "default", nil
	}
	var level Level
	err := decodeValue(setting, &level)
	return level, err
}
