package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
)

type SuppressionReport uint8

const (
	SuppressionsAllowed SuppressionReport = iota
	ReportSuppression
	ReportViolation
	ReportAll
)

func parseSuppressionReport(value string) (SuppressionReport, bool) {
	switch value {
	case "suppression":
		return ReportSuppression, true
	case "violation":
		return ReportViolation, true
	case "all":
		return ReportAll, true
	default:
		return SuppressionsAllowed, false
	}
}

// SuppressionsPolicy controls whether inline and configured exceptions may hide diagnostic
// findings. Its configuration representation is a boolean or a rules/report mapping.
// A nil pointer or zero value permits suppressions. Use DisallowSuppressions
// to construct an enabled policy.
type SuppressionsPolicy struct {
	names     rulemeta.Names
	report    SuppressionReport
	rules     []string
	locations map[string]int
}

// DisallowSuppressions enables restrictions with report set to "suppression",
// "violation", or "all". With no rule IDs, it applies to all suppressible rules.
// It rejects unknown report values and rule IDs, removes duplicate IDs, and owns
// a copy of the selection. Assign the result to Config.Policy.DisallowSuppressions.
func (ruleSet RuleSet) DisallowSuppressions(report string, rules ...string) (*SuppressionsPolicy, error) {
	mode, ok := parseSuppressionReport(report)
	if !ok {
		return nil, fmt.Errorf("unknown suppression report %q; expected suppression, violation, or all", report)
	}
	p := &SuppressionsPolicy{report: mode, names: ruleSet.Names()}
	for _, rule := range rules {
		if !ruleSet.Suppressible(rule) {
			return nil, fmt.Errorf("unknown suppressible rule %q", rule)
		}
		if !slices.Contains(p.rules, rule) {
			p.rules = append(p.rules, rule)
		}
	}
	return p, nil
}

// Enabled reports whether suppression restrictions are enabled.
// Nil and zero-value policies return false.
func (p *SuppressionsPolicy) Enabled() bool {
	return p != nil && p.report != SuppressionsAllowed
}

// Report returns "suppression", "violation", or "all", or an empty string when
// the policy is disabled.
func (p *SuppressionsPolicy) Report() string {
	if p == nil {
		return ""
	}
	switch p.report {
	case ReportSuppression:
		return "suppression"
	case ReportViolation:
		return "violation"
	case ReportAll:
		return "all"
	case SuppressionsAllowed:
		// Allowed suppressions report nothing.
		fallthrough
	default:
		return ""
	}
}

// Rules returns a copy of the explicit rule selection. Nil means all suppressible
// rules when Enabled is true, or no restriction when Enabled is false.
func (p *SuppressionsPolicy) Rules() []string {
	if !p.Enabled() {
		return nil
	}
	return slices.Clone(p.rules)
}

// DecodeConfig validates the shared configuration value. Each successful decode replaces the
// previous rule selection and reporting mode.
func (p *SuppressionsPolicy) DecodeConfig(value any) error {
	next := SuppressionsPolicy{report: ReportAll, locations: make(map[string]int)}
	switch selected := value.(type) {
	case bool:
		if !selected {
			next.report = SuppressionsAllowed
		}
	case map[string]any:
		for key, value := range selected {
			switch key {
			case "report":
				name, ok := value.(string)
				if !ok {
					return binding.At(key, errors.New("report must be suppression, violation, or all"))
				}
				mode, ok := parseSuppressionReport(name)
				if !ok {
					return binding.At(key, fmt.Errorf("unknown report %q; expected suppression, violation, or all", name))
				}
				next.report = mode
			case "rules":
				var rules []string
				if err := binding.Decode(value, &rules); err != nil {
					return binding.At(key, err)
				}
				if len(rules) == 0 {
					return binding.At(key, errors.New("rules must be a nonempty list of suppressible rule IDs"))
				}
				for i, rule := range rules {
					if strings.TrimSpace(rule) == "" {
						return binding.At(key, binding.At(strconv.Itoa(i), fmt.Errorf("unknown suppressible rule %q", rule)))
					}
					if !slices.Contains(next.rules, rule) {
						next.rules = append(next.rules, rule)
						next.locations[rule] = i
					}
				}
			default:
				return binding.At(key, fmt.Errorf("unknown suppression policy key %q", key))
			}
		}
	default:
		return errors.New("disallow-suppressions must be a boolean or mapping")
	}
	*p = next
	return nil
}

func (p *SuppressionsPolicy) ReportFor(rule string) SuppressionReport {
	if p == nil {
		return SuppressionsAllowed
	}
	if len(p.rules) != 0 && !slices.ContainsFunc(p.rules, func(name string) bool {
		return p.names.ResolveAlias(name) == p.names.ResolveAlias(rule)
	}) {
		return SuppressionsAllowed
	}
	return p.report
}
