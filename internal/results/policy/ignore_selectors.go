package policy

import (
	"fmt"
	"strings"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results"
)

// IgnoreSelectors matches exact rule names, diagnostic codes, or rule/code pairs.
// A bare diagnostic code matches that code from any rule.
type IgnoreSelectors struct {
	selectors []string
}

func CompileIgnoreSelectors(values []string) (IgnoreSelectors, error) {
	for _, value := range values {
		rule, code, qualified := strings.Cut(value, "/")
		if rule == "" || strings.ContainsAny(value, " \t\r\n,") || qualified && code == "" {
			return IgnoreSelectors{}, fmt.Errorf("invalid --ignore selector %q: use a rule name, diagnostic code, or rule/code", value)
		}
	}
	return IgnoreSelectors{selectors: append([]string(nil), values...)}, nil
}

func (selectors IgnoreSelectors) matches(rules repoconfig.RuleSet, diagnostic results.Diagnostic) bool {
	for _, selector := range selectors.selectors {
		if rules.ResolveAlias(selector) == rules.ResolveAlias(diagnostic.Rule) || diagnostic.Code != "" && selector == diagnostic.Code {
			return true
		}
		rule, code, qualified := strings.Cut(selector, "/")
		if qualified && rules.ResolveAlias(rule) == rules.ResolveAlias(diagnostic.Rule) && code == diagnostic.Code {
			return true
		}
	}
	return false
}
