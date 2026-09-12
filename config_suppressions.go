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

// SuppressionsPolicy controls whether inline exceptions may hide cache policy
// findings. Its YAML representation is a boolean or a rules/report mapping.
type SuppressionsPolicy struct {
	report suppressionReport
	rules  []string
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
				switch value.Value {
				case "suppression":
					next.report = reportSuppression
				case "violation":
					next.report = reportViolation
				case "all":
					next.report = reportAll
				default:
					return suppressionConfigError(value, fmt.Sprintf("unknown report %q; expected suppression, violation, or all", value.Value))
				}
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
