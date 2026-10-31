package matrix

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func checkResolution(resolution Resolution, mixedTypes bool) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	appendFinding := func(position diagnostic.Position, rule, format string, args ...any) {
		findings = append(findings, diagnostic.Diagnostic{Start: position, Rule: rule, Message: fmt.Sprintf(format, args...)})
	}
	for _, axis := range resolution.axes {
		seen := []value.Value{}
		for _, candidate := range axis.Values {
			duplicate := false
			for _, previous := range seen {
				if dynamic(previous) != dynamic(candidate) {
					continue
				}
				if semanticEqual(previous, candidate) {
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
	for issue := range resolution.exclusionIssues() {
		switch issue := issue.(type) {
		case exclusionWithoutAxes:
			appendFinding(issue.origin.Span.Start, "matrix", `"exclude" section exists but no matrix variation exists`)
		case emptyExclusion:
			appendFinding(issue.origin.Span.Start, "matrix", `matrix filter in "exclude" must not be empty`)
		case unknownExclusionAxis:
			names := make([]string, len(resolution.axes))
			for i, axis := range resolution.axes {
				names[i] = fmt.Sprintf("%q", axis.Name)
			}
			appendFinding(issue.origin.Span.Start, "matrix", "%q in \"exclude\" section does not exist in matrix. available matrix configurations are %s", issue.name, strings.Join(names, ", "))
		}
	}
	if resolution.state == value.Dynamic || resolution.state == value.Invalid {
		return findings
	}
	for _, filter := range resolution.exclude {
		for name, expected := range filter.Values() {
			index := axisIndex(resolution.axes, name)
			if index < 0 {
				continue
			}
			axis := resolution.axes[index]
			if len(axis.Values) == 0 {
				continue
			}
			matched := false
			for _, candidate := range axis.Values {
				if Subset(candidate, expected) {
					matched = true
				}
			}
			if !matched && expected.Known() {
				appendFinding(expected.Origin().Span.Start, "matrix", "value %s in \"exclude\" does not match in matrix %q combinations. possible values are %s", display(expected), name, axisDisplay(axis.Values))
			}
		}
	}
	if mixedTypes {
		for _, filter := range append(slices.Clone(resolution.include), resolution.exclude...) {
			for name, expected := range filter.values {
				if index := axisIndex(resolution.axes, name); index >= 0 {
					for _, candidate := range resolution.axes[index].Values {
						if typeMismatch(candidate, expected) {
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

func dynamic(v value.Value) bool {
	if v.State() == value.Dynamic || v.State() == value.Invalid {
		return true
	}
	if v.Kind() == value.Array {
		for item := range v.Items() {
			if dynamic(item) {
				return true
			}
		}
	}
	if v.Kind() == value.Object {
		for _, item := range v.Props() {
			if dynamic(item) {
				return true
			}
		}
	}
	return false
}

func semanticEqual(left, right value.Value) bool {
	if left.State() == value.Dynamic || right.State() == value.Dynamic {
		return left.State() == value.Dynamic && right.State() == value.Dynamic && left.Source() == right.Source()
	}
	if left.Kind() == value.Array && right.Kind() == value.Array && left.Known() && right.Known() {
		if left.Len() != right.Len() {
			return false
		}
		for i := range left.Len() {
			a, _ := left.Index(i)
			b, _ := right.Index(i)
			if !semanticEqual(a, b) {
				return false
			}
		}
		return true
	}
	if left.Kind() == value.Object && right.Kind() == value.Object && left.Known() && right.Known() {
		if left.Len() != right.Len() {
			return false
		}
		for name, a := range left.Props() {
			b, ok := right.Prop(name)
			if !ok || !semanticEqual(a, b) {
				return false
			}
		}
		return true
	}
	return left.Equal(right)
}

// display renders a matrix value for diagnostics. Non-string YAML literals
// keep their source spelling (TRUE, ~, +1.2e2) so the message quotes what the
// user wrote; strings are always quoted from their decoded text.
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

func typeMismatch(axis, filter value.Value) bool {
	if !filter.Known() {
		return false
	}
	if axis.State() == value.Invalid {
		return false
	}
	if axis.State() == value.Dynamic {
		return unknownTypeMismatch(filter)
	}
	if filter.Kind() == value.Object {
		for name, expected := range filter.Props() {
			if axis.Kind() == value.Array {
				if index, err := matrixIndex(name); err == nil && dynamicIndex(axis, index) {
					continue
				}
			}
			actual, ok := lookup(axis, name)
			if ok && typeMismatch(actual, expected) || !ok && missingTypeMismatch(expected) {
				return true
			}
		}
		return false
	}
	if filter.Kind() == value.Array {
		for i := range filter.Len() {
			expected, _ := filter.Index(i)
			if expected.State() == value.Dynamic || expected.State() == value.Invalid {
				return false
			}
			uncertain := axis.Kind() == value.Array && dynamicIndex(axis, i)
			if uncertain {
				return false
			}
			actual, aok := axis.Index(i)
			if aok && typeMismatch(actual, expected) || !aok && missingTypeMismatch(expected) {
				return true
			}
		}
		return false
	}
	if !axis.Known() {
		return false
	}
	if axis.Kind() == value.Array || axis.Kind() == value.Object {
		return false
	}
	if axis.Kind() == filter.Kind() {
		return false
	}
	if axis.Kind() == value.String && filter.Kind() == value.String {
		return false
	}
	return true
}

func missingTypeMismatch(filter value.Value) bool {
	if !filter.Known() || filter.Kind() == value.Null {
		return false
	}
	if filter.Kind() == value.Object {
		for _, nested := range filter.Props() {
			if missingTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	if filter.Kind() == value.Array {
		for nested := range filter.Items() {
			if missingTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	return true
}

func unknownTypeMismatch(filter value.Value) bool {
	if !filter.Known() {
		return false
	}
	if filter.Kind() == value.Object {
		for _, nested := range filter.Props() {
			if unknownTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	if filter.Kind() == value.Array {
		for nested := range filter.Items() {
			if nested.State() == value.Dynamic || nested.State() == value.Invalid {
				return false
			}
			if unknownTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	return filter.Kind() == value.Bool || filter.Kind() == value.Number
}
