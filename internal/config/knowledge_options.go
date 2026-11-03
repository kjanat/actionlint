package config

// projectSettings prepares internal policy settings from the selected rules.
func projectSettings(c *Config) {
	for _, descriptor := range Rules() {
		setting, exists := canonicalSetting(c.Lint.Rules.Groups()[descriptor.Category], descriptor.Name)
		if !exists {
			legacy := LegacyGroup(descriptor.Name)
			group, removed := c.Lint.Rules.LegacyGroups()[legacy]
			if !removed {
				group = c.Lint.Rules.Groups()[legacy]
			}
			setting, exists = canonicalSetting(group, descriptor.Name)
		}
		if !exists || descriptor.Configure == nil {
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
		descriptor.Configure(c, setting)
		if setting.Level() == "off" {
			switch descriptor.Name {
			case "job-timeout":
				c.Policy.RequireJobTimeout = &JobTimeoutPolicy{}
			case "explicit-permissions":
				c.Policy.RequirePermissions = &PermissionsPolicy{}
			case "forbidden-suppression":
				c.Policy.DisallowSuppressions = &SuppressionsPolicy{}
			}
		}
	}
	if c.Lint.MessageIgnores != nil {
		c.Paths = make(map[string]PathConfig, len(c.Lint.MessageIgnores))
		for path, patterns := range c.Lint.MessageIgnores {
			c.Paths[path] = PathConfig{Ignore: patterns}
		}
	}
}
