package actionlint

import (
	"fmt"
	"slices"

	"go.yaml.in/yaml/v4"
)

type suppressionReport uint8

const (
	suppressionsAllowed suppressionReport = iota
	reportSuppression
	reportViolation
	reportAll
)

func parseSuppressionReport(value string) (suppressionReport, bool) {
	switch value {
	case "suppression":
		return reportSuppression, true
	case "violation":
		return reportViolation, true
	case "all":
		return reportAll, true
	default:
		return suppressionsAllowed, false
	}
}

// SuppressionsPolicy controls whether inline exceptions may hide cache policy
// findings. Its YAML representation is a boolean or a rules/report mapping.
// A nil pointer or zero value permits inline suppressions. Use DisallowSuppressions
// to construct an enabled policy.
type SuppressionsPolicy struct {
	report suppressionReport
	rules  []string
}

// DisallowSuppressions enables restrictions with report set to "suppression",
// "violation", or "all". With no rule IDs, it applies to all suppressible rules.
// It rejects unknown report values and rule IDs, removes duplicate IDs, and owns
// a copy of the selection. Assign the result to Config.Policy.DisallowSuppressions.
func DisallowSuppressions(report string, rules ...string) (*SuppressionsPolicy, error) {
	mode, ok := parseSuppressionReport(report)
	if !ok {
		return nil, fmt.Errorf("unknown suppression report %q; expected suppression, violation, or all", report)
	}
	p := &SuppressionsPolicy{report: mode}
	for _, rule := range rules {
		if !isInlineSuppressibleRule(rule) {
			return nil, fmt.Errorf("unknown suppressible rule %q", rule)
		}
		if !slices.Contains(p.rules, rule) {
			p.rules = append(p.rules, rule)
		}
	}
	return p, nil
}

// Enabled reports whether inline suppression restrictions are enabled.
// Nil and zero-value policies return false.
func (p *SuppressionsPolicy) Enabled() bool {
	return p != nil && p.report != suppressionsAllowed
}

// Report returns "suppression", "violation", or "all", or an empty string when
// the policy is disabled.
func (p *SuppressionsPolicy) Report() string {
	if p == nil {
		return ""
	}
	switch p.report {
	case reportSuppression:
		return "suppression"
	case reportViolation:
		return "violation"
	case reportAll:
		return "all"
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

// UnmarshalYAML implements yaml.Unmarshaler. Each successful decode replaces the
// previous rule selection and reporting mode.
func (p *SuppressionsPolicy) UnmarshalYAML(n *yaml.Node) error {
	next := SuppressionsPolicy{report: reportAll}
	switch {
	case n.Kind == yaml.ScalarNode && n.Tag == "!!bool":
		var enabled bool
		if err := n.Decode(&enabled); err != nil {
			return err
		}
		if !enabled {
			next.report = suppressionsAllowed
		}
	case n.Kind == yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if seen[key.Value] {
				return suppressionConfigError(key, fmt.Sprintf("duplicate key %q", key.Value))
			}
			seen[key.Value] = true
			switch key.Value {
			case "report":
				if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
					return suppressionConfigError(value, "report must be suppression, violation, or all")
				}
				mode, ok := parseSuppressionReport(value.Value)
				if !ok {
					return suppressionConfigError(value, fmt.Sprintf("unknown report %q; expected suppression, violation, or all", value.Value))
				}
				next.report = mode
			case "rules":
				if value.Kind != yaml.SequenceNode || len(value.Content) == 0 {
					return suppressionConfigError(value, "rules must be a nonempty list of suppressible rule IDs")
				}
				for _, rule := range value.Content {
					if rule.Kind != yaml.ScalarNode || rule.Tag != "!!str" || !isInlineSuppressibleRule(rule.Value) {
						return suppressionConfigError(rule, fmt.Sprintf("unknown suppressible rule %q", rule.Value))
					}
					if !slices.Contains(next.rules, rule.Value) {
						next.rules = append(next.rules, rule.Value)
					}
				}
			default:
				return suppressionConfigError(key, fmt.Sprintf("unknown key %q", key.Value))
			}
		}
	default:
		return suppressionConfigError(n, "expected a boolean or mapping")
	}
	*p = next
	return nil
}

func suppressionConfigError(n *yaml.Node, message string) error {
	return fmt.Errorf("yaml: disallow-suppressions: %s at line:%d,col:%d", message, n.Line, n.Column)
}

func (p *SuppressionsPolicy) reportFor(rule string) suppressionReport {
	if p == nil || (len(p.rules) != 0 && !slices.Contains(p.rules, rule)) {
		return suppressionsAllowed
	}
	return p.report
}
