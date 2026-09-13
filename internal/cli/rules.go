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
	for _, rule := range rules {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", rule.Name, rule.Category, rule.Description); err != nil {
			return err
		}
	}
	return w.Flush()
}
