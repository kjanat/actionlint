package backends

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

type diagnosticKey struct {
	filename, code, message, severity, source string
	effect                                    scriptengine.TemplateEffect
	start, end                                sourcetext.Position
}

// A diagnostic that only some variants report names their values in its message.
func mergeVariants(prepared preparation.Prepared, results []outcome) outcome {
	if len(prepared.Variants) == 0 {
		return results[0]
	}
	type reported struct {
		diagnostic scriptengine.Diagnostic
		variants   []int
	}
	merged := outcome{skipped: true}
	index := map[diagnosticKey]int{}
	var found []reported
	failures := map[string]struct{}{}
	sources := map[string]struct{}{}
	for variant, result := range results {
		merged.skipped = merged.skipped && result.skipped
		if result.directory != "" {
			merged.directory = result.directory
		}
		if result.copies != nil && merged.copies == nil {
			merged.copies = map[string]reportedFile{}
		}
		for name, file := range result.copies {
			if _, seen := merged.copies[name]; !seen {
				merged.copies[name] = file
			}
		}
		for _, source := range result.sources {
			if _, seen := sources[source]; !seen {
				sources[source] = struct{}{}
				merged.sources = append(merged.sources, source)
			}
		}
		if result.err != nil {
			if _, seen := failures[result.err.Error()]; !seen {
				failures[result.err.Error()] = struct{}{}
				merged.err = errors.Join(merged.err, result.err)
			}
		}
		for _, d := range result.diagnostics {
			d = variantDiagnostic(prepared.Variants[variant], prepared.LineMap, d)
			key := diagnosticKey{filename: d.Filename, code: d.Code, message: d.Message, severity: d.Severity, source: d.Source, effect: d.TemplateEffect, start: d.Location, end: d.EndLocation}
			if i, seen := index[key]; seen {
				found[i].variants = append(found[i].variants, variant)
				continue
			}
			index[key] = len(found)
			found = append(found, reported{diagnostic: d, variants: []int{variant}})
		}
	}
	for _, f := range found {
		if len(f.variants) < len(results) {
			if condition := variantCondition(prepared.Variants, f.variants, diagnosticRows(prepared.LineMap, f.diagnostic)); condition != "" {
				f.diagnostic.Message += " (when " + condition + ")"
			}
		}
		merged.diagnostics = append(merged.diagnostics, f.diagnostic)
	}
	return merged
}

func variantDiagnostic(variant preparation.Variant, lines func(int) preparation.Line, d scriptengine.Diagnostic) scriptengine.Diagnostic {
	if d.Source != "" {
		return d
	}
	row := scriptRow(lines)
	if line, ok := row(d.Location.Line); ok {
		d.Location.Column = variant.StartColumn(line, d.Location.Column)
	}
	if line, ok := row(d.EndLocation.Line); ok && d.EndLocation != (sourcetext.Position{}) {
		d.EndLocation.Column = variant.EndColumn(line, d.EndLocation.Column)
	}
	return d
}

func diagnosticRows(lines func(int) preparation.Line, d scriptengine.Diagnostic) func(int) bool {
	row := scriptRow(lines)
	first, ok := row(d.Location.Line)
	if d.Source != "" || !ok {
		return func(int) bool { return false }
	}
	last := first
	if end, ok := row(d.EndLocation.Line); ok && d.EndLocation != (sourcetext.Position{}) && end > first {
		last = end
	}
	return func(r int) bool { return r >= first && r <= last }
}

func variantCondition(variants []preparation.Variant, reporting []int, rows func(int) bool) string {
	near := false
	for _, s := range variants[reporting[0]].Substitutions {
		near = near || s.Alternatives > 1 && rows(s.Row)
	}
	var conditions []string
	for _, variant := range reporting {
		var values []string
		for _, s := range variants[variant].Substitutions {
			if s.Alternatives > 1 && (!near || rows(s.Row)) {
				values = append(values, s.Template+" is "+strconv.Quote(s.Value))
			}
		}
		if condition := strings.Join(values, " and "); condition != "" && !slices.Contains(conditions, condition) {
			conditions = append(conditions, condition)
		}
	}
	return strings.Join(conditions, " or ")
}

func scriptRow(lines func(int) preparation.Line) func(int) (int, bool) {
	return func(line int) (int, bool) {
		if lines == nil {
			return line, true
		}
		mapped := lines(line)
		return mapped.Line, mapped.Origin == preparation.LineScript
	}
}
