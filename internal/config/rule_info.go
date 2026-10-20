package config

import (
	"actionlint.kjanat.dev/internal/rulecatalog"
	"actionlint.kjanat.dev/rule"
)

type RuleInfo = rule.Descriptor

func RuleInfos() []RuleInfo { return rulecatalog.Rules() }
