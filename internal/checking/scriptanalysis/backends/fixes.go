package backends

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// scriptFix maps an engine's suggested replacements onto the workflow file.
// Only literal YAML blocks permit direct text edits without changing quoting
// or folding. Each replacement must stay on one script row the engine saw
// unchanged: a row preparation generated, or one whose expressions it
// masked, makes the whole fix unavailable, as do indentation changes and
// overlapping replacements. A fix may edit several rows.
func scriptFix(source *definition.ScriptSource, prepared preparation.Prepared, fix scriptengine.Fix) (results.Fix, bool) {
	if source == nil || !source.Literal || len(fix.Replacements) == 0 {
		return results.Fix{}, false
	}
	seen := strings.Split(prepared.Source, "\n")
	edits := make([]results.Edit, 0, len(fix.Replacements))
	for _, replacement := range fix.Replacements {
		line := replacement.Start.Line
		if prepared.LineMap != nil {
			mapped := prepared.LineMap(replacement.Start.Line)
			if mapped.Origin != preparation.LineScript {
				return results.Fix{}, false
			}
			line = mapped.Line
		}
		if replacement.Start.Line != replacement.End.Line || line < 1 || line > len(source.Lines) || replacement.Start.Line < 1 || replacement.Start.Line > len(seen) ||
			strings.ContainsAny(replacement.Text, "\x00\r\n") || !utf8.ValidString(replacement.Text) {
			return results.Fix{}, false
		}
		bounds := source.Lines[line-1]
		original := source.Value[bounds.Start:bounds.End]
		if original != seen[replacement.Start.Line-1] {
			return results.Fix{}, false
		}
		startOffset, startOK := sourcetext.ByteOffsetAtColumn(original, replacement.Start.Column)
		endOffset, endOK := sourcetext.ByteOffsetAtColumn(original, replacement.End.Column)
		if !startOK || !endOK || startOffset > endOffset {
			return results.Fix{}, false
		}
		// Require both boundaries in the same exact span. Insertions at the end
		// of a line are valid even though they are not diagnostic start positions.
		var mapped *sourcemap.Span
		for _, span := range source.Spans {
			if bounds.Start+startOffset >= span.Start && bounds.Start+endOffset <= span.End {
				mapped = &span
				break
			}
		}
		if mapped == nil || mapped.Start == mapped.End {
			return results.Fix{}, false
		}
		start := mapped.Document.Column + utf8.RuneCountInString(source.Value[mapped.Start:bounds.Start+startOffset])
		end := mapped.Document.Column + utf8.RuneCountInString(source.Value[mapped.Start:bounds.Start+endOffset])
		edits = append(edits, results.Edit{
			Start:       sourcetext.Position{Line: mapped.Document.Line, Column: start},
			End:         sourcetext.Position{Line: mapped.Document.Line, Column: end},
			Replacement: replacement.Text,
		})
	}
	slices.SortFunc(edits, func(a, b results.Edit) int {
		if a.Start.Line != b.Start.Line {
			return a.Start.Line - b.Start.Line
		}
		return a.Start.Column - b.Start.Column
	})
	for i, edit := range edits {
		if i > 0 {
			previous := edits[i-1]
			if previous.End.Line == edit.Start.Line && previous.End.Column >= edit.Start.Column {
				// Shared boundaries depend on the engine's insertion precedence;
				// exposing them as unordered source edits would lose that meaning.
				return results.Fix{}, false
			}
		}
	}
	if !fixPreservesIndent(source, edits) {
		return results.Fix{}, false
	}
	return results.Fix{Description: fix.Description, Applicability: fix.Applicability, Edits: edits}, true
}

func fixPreservesIndent(source *definition.ScriptSource, edits []results.Edit) bool {
	for _, span := range source.Spans {
		original := source.Value[span.Start:span.End]
		modified := original
		for _, edit := range slices.Backward(edits) {
			if edit.Start.Line != span.Document.Line {
				continue
			}
			start, startOK := sourcetext.ByteOffsetAtColumn(original, edit.Start.Column-span.Document.Column+1)
			end, endOK := sourcetext.ByteOffsetAtColumn(original, edit.End.Column-span.Document.Column+1)
			if !startOK || !endOK {
				return false
			}
			modified = modified[:start] + edit.Replacement + modified[end:]
		}
		prefix := func(line string) string { return line[:len(line)-len(strings.TrimLeft(line, " \t"))] }
		if prefix(original) != prefix(modified) || (strings.TrimSpace(original) != "" && strings.TrimSpace(modified) == "") {
			return false
		}
	}
	return true
}
