package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"

	rulemetadata "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func commandRules() []rulemetadata.Descriptor {
	rules := checking.ConfigurationRules().RuleInfos()
	slices.SortFunc(rules, func(a, b rulemetadata.Descriptor) int { return strings.Compare(a.Name, b.Name) })
	return rules
}

func writeRules(out io.Writer, name string, asJSON bool, rules []rulemetadata.Descriptor) error {
	if name != "" {
		for _, rule := range rules {
			if rule.Name == name {
				if asJSON {
					return commandstreams.WriteJSON(out, rule)
				}
				_, err := fmt.Fprintf(out, "%s (%s/%s)\n\n%s\n\n%s\n\nProvider: %s; Maturity: %s\n%s\n", rule.Name, rule.Category, rule.Subcategory, rule.Explanation, rule.Help, rule.Provider, rule.Maturity, rule.DocumentationURL)
				if err != nil {
					return err
				}
				group := checking.RuleCategory(rule.Name)
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
		return commandargs.UsageError{Cause: fmt.Errorf("unknown rule %q; run actionlint rules to list checks", name)}
	}
	if asJSON {
		return commandstreams.WriteJSON(out, rules)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	groups := map[string][]rulemetadata.Descriptor{}
	for _, rule := range rules {
		category := rule.Category
		if category == "" {
			category = "uncategorized"
		}
		groups[category] = append(groups[category], rule)
	}
	order := rulemetadata.Categories()
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
