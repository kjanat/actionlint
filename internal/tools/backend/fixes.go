package backend

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// scriptFix maps an engine's suggested replacements onto the workflow file.
// Only literal YAML blocks permit direct text edits without changing quoting
// or folding. Each replacement must stay on one script row the engine saw
// unchanged: a row preparation generated, or one whose expressions it
// masked, makes the whole fix unavailable, as do indentation changes and
// overlapping replacements. A fix may edit several rows.
func scriptFix(source workflowdecl.ScriptSourceView, prepared profile.Prepared, fix analyzer.ScriptFix) (diagnostic.Fix, bool) {
	if !source.Present() || !source.Literal() || len(fix.Replacements) == 0 {
		return diagnostic.Fix{}, false
	}
	seen := strings.Split(prepared.Source, "\n")
	edits := make([]diagnostic.Edit, 0, len(fix.Replacements))
	for _, replacement := range fix.Replacements {
		line := replacement.Row
		if prepared.LineMap != nil {
			mapped := prepared.LineMap(replacement.Row)
			if mapped.Origin != profile.LineScript {
				return diagnostic.Fix{}, false
			}
			line = mapped.Line
		}
		if replacement.Row != replacement.EndRow || line < 1 || line > source.LinesLen() || replacement.Row < 1 || replacement.Row > len(seen) ||
			strings.ContainsAny(replacement.Text, "\x00\r\n") || !utf8.ValidString(replacement.Text) {
			return diagnostic.Fix{}, false
		}
		bounds, _ := source.Line(line - 1)
		original := source.Value()[bounds.Start:bounds.End]
		if original != seen[replacement.Row-1] {
			return diagnostic.Fix{}, false
		}
		startOffset, startOK := text.ByteOffsetAtColumn(original, replacement.Column)
		endOffset, endOK := text.ByteOffsetAtColumn(original, replacement.EndColumn)
		if !startOK || !endOK || startOffset > endOffset {
			return diagnostic.Fix{}, false
		}
		// Require both boundaries in the same exact span. Insertions at the end
		// of a line are valid even though they are not diagnostic start positions.
		var mapped *workflowlang.ScriptSourceSpan
		for span := range source.Spans() {
			if bounds.Start+startOffset >= span.Start && bounds.Start+endOffset <= span.End {
				mapped = &span
				break
			}
		}
		if mapped == nil || mapped.Start == mapped.End {
			return diagnostic.Fix{}, false
		}
		start := mapped.YamlColumn + utf8.RuneCountInString(source.Value()[mapped.Start:bounds.Start+startOffset])
		end := mapped.YamlColumn + utf8.RuneCountInString(source.Value()[mapped.Start:bounds.Start+endOffset])
		edits = append(edits, diagnostic.Edit{
			Start:       diagnostic.Position{Line: mapped.YamlLine, Column: start},
			End:         diagnostic.Position{Line: mapped.YamlLine, Column: end},
			Replacement: replacement.Text,
		})
	}
	slices.SortFunc(edits, func(a, b diagnostic.Edit) int {
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
				return diagnostic.Fix{}, false
			}
		}
	}
	if !fixPreservesIndent(source, edits) {
		return diagnostic.Fix{}, false
	}
	return diagnostic.Fix{Description: fix.Description, Applicability: "suggested", Edits: edits}, true
}

func fixPreservesIndent(source workflowdecl.ScriptSourceView, edits []diagnostic.Edit) bool {
	for span := range source.Spans() {
		original := source.Value()[span.Start:span.End]
		modified := original
		for _, edit := range slices.Backward(edits) {
			if edit.Start.Line != span.YamlLine {
				continue
			}
			start, startOK := text.ByteOffsetAtColumn(original, edit.Start.Column-span.YamlColumn+1)
			end, endOK := text.ByteOffsetAtColumn(original, edit.End.Column-span.YamlColumn+1)
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
