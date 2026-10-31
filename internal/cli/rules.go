package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
)

func commandRules() []repoconfig.RuleInfo {
	rules := repoconfig.RuleInfos()
	slices.SortFunc(rules, func(a, b repoconfig.RuleInfo) int { return strings.Compare(a.Name, b.Name) })
	return rules
}

func writeRules(out io.Writer, name string, asJSON bool, rules []repoconfig.RuleInfo) error {
	if name != "" {
		for _, rule := range rules {
			if rule.Name == name {
				if asJSON {
					return writeCommandJSON(out, rule)
				}
				_, err := fmt.Fprintf(out, "%s (%s/%s)\n\n%s\n\n%s\n\nProvider: %s; Maturity: %s; Fixes: %s\n%s\n", rule.Name, rule.Category, rule.Subcategory, rule.Explanation, rule.Help, rule.Provider, rule.Maturity, rule.FixAvailability, rule.DocumentationURL)
				if err != nil {
					return err
				}
				group := rulecatalog.ConfigGroup(rule.Name)
				if group == "" {
					group = "plugins"
				}
				_, err = fmt.Fprintf(out, "Configuration: lint.rules.%s.%s\n", group, rule.Name)
				if err != nil {
					return err
				}
				for _, example := range rule.Examples {
					if _, err := fmt.Fprintf(out, "\n%s\n\nBefore:\n%s\nAfter:\n%s\n", example.Description, example.Before, example.After); err != nil {
						return err
					}
				}
				return nil
			}
		}
		return commandUsageError{fmt.Errorf("unknown rule %q; run actionlint rules to list checks", name)}
	}
	if asJSON {
		return writeCommandJSON(out, rules)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	groups := map[string][]repoconfig.RuleInfo{}
	for _, rule := range rules {
		category := rule.Category
		if category == "" {
			category = "uncategorized"
		}
		groups[category] = append(groups[category], rule)
	}
	order := make([]string, 0, 6+len(groups))
	order = append(order, "correctness", "security", "suspicious", "style", "policy", "external")
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
