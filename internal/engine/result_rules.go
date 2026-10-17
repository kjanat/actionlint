package engine

import (
	"actionlint.kjanat.dev/internal/builtin"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/rule"
)

func templateRules() []rule.Descriptor {
	var rules []rule.Descriptor
	for _, descriptor := range builtin.Descriptors() {
		if descriptor.Provider == "actionlint" && descriptor.Build == nil && descriptor.Script == nil {
			rules = append(rules, descriptor.Clone())
		}
	}
	return rules
}

// RuleInfo carries catalogue metadata without executable rule state.
type RuleInfo = repoconfig.RuleInfo
