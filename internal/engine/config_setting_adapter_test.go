package engine

import repoconfig "actionlint.kjanat.dev/internal/config"

func ruleSetting(cfg *Config, d ruleDescriptor, p RulePresets) RuleSetting {
	return repoconfig.SettingFor(cfg, repoconfig.Rule{Descriptor: d.Descriptor, Group: d.Group}, p, d.selected)
}
