package engine

import "actionlint.kjanat.dev/rule"

func templateRules() []rule.Descriptor {
	var rules []rule.Descriptor
	for _, descriptor := range builtinRuleDescriptors() {
		if descriptor.Provider == "actionlint" && descriptor.build == nil && descriptor.buildScripts == nil {
			rules = append(rules, descriptor.Clone())
		}
	}
	return rules
}
