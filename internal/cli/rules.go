package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"actionlint.kjanat.dev"
)

func commandRules() []actionlint.RuleInfo {
	rules := actionlint.BuiltinRules()
	slices.SortFunc(rules, func(a, b actionlint.RuleInfo) int { return strings.Compare(a.Name, b.Name) })
	return rules
}

func writeRules(out io.Writer, name string, asJSON bool) error {
	rules := commandRules()
	if name != "" {
		for _, rule := range rules {
			if rule.Name == name {
				if asJSON {
					return writeCommandJSON(out, rule)
				}
				_, err := fmt.Fprintf(out, "%s (%s)\n\n%s\n", rule.Name, rule.Category, rule.Description)
				return err
			}
		}
		return commandUsageError{fmt.Errorf("unknown rule %q; run actionlint rules to list checks", name)}
	}
	if asJSON {
		return writeCommandJSON(out, rules)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	groups := map[string][]actionlint.RuleInfo{}
	for _, rule := range rules {
		groups[rule.Category] = append(groups[rule.Category], rule)
	}
	order := make([]string, 0, 5+len(groups))
	order = append(order, "correctness", "security", "suspicious", "policy", "external")
	var other []string
	for category := range groups {
		if !slices.Contains(order, category) {
			other = append(other, category)
		}
	}
	slices.Sort(other)
	order = append(order, other...)
	first := true
	for _, category := range order {
		if len(groups[category]) == 0 {
			continue
		}
		if !first {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		first = false
		title := strings.ToUpper(category[:1]) + category[1:]
		if category == "external" {
			title = "External linters"
		}
		if _, err := fmt.Fprintln(w, title+":"); err != nil {
			return err
		}
		for _, rule := range groups[category] {
			if _, err := fmt.Fprintf(w, "  %s\t%s\n", rule.Name, rule.Description); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}
