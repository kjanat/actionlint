package configuration

// projectSettings prepares internal policy settings from the selected rules.
func (ruleSet RuleSet) projectSettings(c *Config) {
	c.Lint.Rules.ruleSet = ruleSet

	for _, descriptor := range ruleSet.Rules() {
		setting, exists := ruleSet.canonicalSetting(c.Lint.Rules.Groups()[descriptor.Category], descriptor.Name)
		if !exists {
			legacy := ruleSet.LegacyGroup(descriptor.Name)
			group, removed := c.Lint.Rules.LegacyGroups()[legacy]
			if !removed {
				group = c.Lint.Rules.Groups()[legacy]
			}
			setting, exists = ruleSet.canonicalSetting(group, descriptor.Name)
		}
		policy, projected := descriptor.policySetting()
		if !exists || !projected {
			continue
		}
		if (setting.Level() == "" || setting.Level() == "default") && !setting.HasOptions() {
			continue
		}
		if setting.Level() == "" || setting.Level() == "default" {
			setting = setting.WithLevel("off")
			if descriptor.Recommended {
				setting = setting.WithLevel("on")
			}
		}
		policy.configure(c, setting)
		if setting.Level() == "off" && policy.disable != nil {
			policy.disable(c)
		}
	}
	if c.Lint.MessageIgnores != nil {
		c.Paths = make(map[string]PathConfig, len(c.Lint.MessageIgnores))
		for path, patterns := range c.Lint.MessageIgnores {
			c.Paths[path] = PathConfig{Ignore: patterns}
		}
	}
}
