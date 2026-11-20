package output

import (
	"crypto/sha256"
	"fmt"
	"io"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func printDependencySummary(out io.Writer, diagnostics []results.PublishedDiagnostic) error {
	type sourceKey struct {
		action, unknown string
		digest          [sha256.Size]byte
	}
	type dependency struct {
		pins                    []string
		errors, warnings, infos int
	}
	groups := make(map[sourceKey]*dependency)
	var order []sourceKey
	for _, d := range diagnostics {
		if d.Origin != results.DependencyOrigin {
			continue
		}
		key := sourceKey{action: d.Path}
		source, pin, recognized := results.DependencyAddress(d.Path)
		if recognized {
			key.action = strings.TrimSuffix(strings.TrimSuffix(source, "/action.yml"), "/action.yaml")
		}
		if d.Source.Content == nil {
			key.unknown = d.Path
		} else {
			key.digest = sha256.Sum256(d.Source.Content)
		}
		group := groups[key]
		if group == nil {
			group = &dependency{}
			groups[key] = group
			order = append(order, key)
		}
		addPin := func(value string) {
			if value != "" && !slices.Contains(group.pins, value) {
				group.pins = append(group.pins, value)
			}
		}
		addPin(pin)
		for _, related := range d.Related {
			if related.Message == results.IdenticalDependencyLocation {
				_, otherPin, ok := results.DependencyAddress(related.Path)
				if ok {
					addPin(otherPin)
				}
			}
		}
		switch d.Severity.Effective() {
		case severity.Error:
			group.errors++
		case severity.Warning:
			group.warnings++
		case severity.Info:
			group.infos++
		}
	}
	slices.SortFunc(order, func(a, b sourceKey) int {
		return strings.Compare(a.action+strings.Join(groups[a].pins, ","), b.action+strings.Join(groups[b].pins, ","))
	})
	for _, key := range order {
		group := groups[key]
		slices.Sort(group.pins)
		label := terminalText(key.action)
		if len(group.pins) > 0 {
			label += "@" + terminalText(strings.Join(group.pins, ", "))
		}
		if _, err := fmt.Fprintf(out, "dependency: %s: %d errors, %d warnings, %d info\n", label, group.errors, group.warnings, group.infos); err != nil {
			return err
		}
	}
	if len(order) > 0 {
		_, err := fmt.Fprint(out, "Show full dependency findings with: actionlint check --dependency-findings=full\n\n")
		return err
	}
	return nil
}
