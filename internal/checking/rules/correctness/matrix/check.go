// Package matrix reports invalid matrix declarations and mixed-type filters.
package matrix

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	matrixmodel "actionlint.kjanat.dev/v2/internal/model/workflows/matrix"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Check reports declaration problems using resolved matrix facts.
func Check(resolution matrixmodel.Resolution, mixedTypes bool) []results.Diagnostic {
	axes := resolution.Axes()
	var findings []results.Diagnostic
	appendFinding := func(position results.Position, rule, format string, args ...any) {
		findings = append(findings, results.Diagnostic{Start: position, Rule: rule, Message: fmt.Sprintf(format, args...)})
	}
	for _, axis := range axes {
		seen := []value.Value{}
		for _, candidate := range axis.Values {
			duplicate := false
			for _, previous := range seen {
				if matrixmodel.ValueHasUnknownParts(previous) != matrixmodel.ValueHasUnknownParts(candidate) {
					continue
				}
				if matrixmodel.EqualDeclaredValues(previous, candidate) {
					pos := previous.Origin().Span.Start
					appendFinding(candidate.Origin().Span.Start, "matrix", "duplicate value %s is found in matrix %q. the same value is at line:%d,col:%d", display(candidate), axis.Name, pos.Line, pos.Column)
					duplicate = true
					break
				}
			}
			if !duplicate {
				seen = append(seen, candidate)
			}
		}
	}
	for issue := range resolution.ExclusionProblems() {
		switch issue := issue.(type) {
		case matrixmodel.ExclusionWithoutAxes:
			appendFinding(issue.Origin.Span.Start, "matrix", `"exclude" section exists but no matrix variation exists`)
		case matrixmodel.EmptyExclusion:
			appendFinding(issue.Origin.Span.Start, "matrix", `matrix filter in "exclude" must not be empty`)
		case matrixmodel.UnknownExclusionAxis:
			names := make([]string, len(axes))
			for i, axis := range axes {
				names[i] = fmt.Sprintf("%q", axis.Name)
			}
			appendFinding(issue.Origin.Span.Start, "matrix", "%q in \"exclude\" section does not exist in matrix. available matrix configurations are %s", issue.Name, strings.Join(names, ", "))
		}
	}
	if resolution.State() == value.Dynamic || resolution.State() == value.Invalid {
		return findings
	}
	for _, filter := range resolution.Exclusions() {
		for name, expected := range filter.Values() {
			axis, exists := resolution.Axis(name)
			if !exists {
				continue
			}
			if len(axis.Values) == 0 {
				continue
			}
			matched := false
			for _, candidate := range axis.Values {
				if matrixmodel.Subset(candidate, expected) {
					matched = true
				}
			}
			if !matched && expected.Known() {
				appendFinding(expected.Origin().Span.Start, "matrix", "value %s in \"exclude\" does not match in matrix %q combinations. possible values are %s", display(expected), name, axisDisplay(axis.Values))
			}
		}
	}
	if mixedTypes {
		for _, filter := range append(resolution.Inclusions(), resolution.Exclusions()...) {
			for name, expected := range filter.Values() {
				if axis, exists := resolution.Axis(name); exists {
					for _, candidate := range axis.Values {
						if matrixmodel.FilterMayCompareDifferentTypes(candidate, expected) {
							appendFinding(expected.Origin().Span.Start, "mixed-type-matrix-filters", "matrix filter for %q compares different or unknown scalar types using Actions loose equality; validate axis types before filtering", name)
							break
						}
					}
				}
			}
		}
	}
	return findings
}

// display renders non-string YAML literals with their source spelling
// (TRUE, ~, +1.2e2) and quotes strings using their decoded text.
func display(v value.Value) string {
	if spelling, ok := v.Spelling(); ok && spelling != "" && v.Kind() != value.String {
		return spelling
	}
	if v.Known() {
		switch v.Kind() {
		case value.Null:
			return "null"
		case value.Array:
			return "[" + axisDisplay(slices.Collect(v.Items())) + "]"
		case value.Object:
			props := maps.Collect(v.Props())
			fields := make([]string, 0, len(props))
			for _, name := range slices.Sorted(maps.Keys(props)) {
				fields = append(fields, fmt.Sprintf("%q: %s", name, display(props[name])))
			}
			return "{" + strings.Join(fields, ", ") + "}"
		case value.KindUnknown, value.Bool, value.Number, value.String:
		}
	}
	if text, ok := v.ScalarString(); ok {
		if v.Kind() == value.String {
			return fmt.Sprintf("%q", text)
		}
		return text
	}
	return v.Source()
}

func axisDisplay(values []value.Value) string {
	texts := make([]string, len(values))
	for i, item := range values {
		texts[i] = display(item)
	}
	return strings.Join(texts, ", ")
}
