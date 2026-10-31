package config

// projectSettings prepares internal policy settings from the selected rules.
func projectSettings(c *Config) {
	for _, descriptor := range Rules() {
		setting, exists := c.Lint.Rules.Groups()[descriptor.Group].Rule(descriptor.Name)
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
			case "require-job-timeout":
				c.Policy.RequireJobTimeout = &JobTimeoutPolicy{}
			case "require-permissions":
				c.Policy.RequirePermissions = &PermissionsPolicy{}
			case "disallow-suppressions":
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
