package config

import (
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/rule"
)

type RuleInfo = rule.Descriptor

func RuleInfos() []RuleInfo { return rulecatalog.Rules() }
