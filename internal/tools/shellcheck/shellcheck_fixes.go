package shellcheck

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/source/text"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

type shellcheckFix struct {
	Replacements []shellcheckReplacement `json:"replacements"`
}

type shellcheckReplacement struct {
	Line        int    `json:"line"`
	EndLine     int    `json:"endLine"`
	Column      int    `json:"column"`
	EndColumn   int    `json:"endColumn"`
	Replacement string `json:"replacement"`
}

// ShellCheck json1 already converts tab stops to character columns. Only literal
// YAML blocks permit direct text edits without changing quoting or folding.
// Each replacement must stay on one unchanged source line; a fix can edit several
// lines, but generated lines, expressions, indentation changes, and overlapping
// replacements make the entire fix unavailable.
func shellcheckDiagnosticFixes(source documentparse.ScriptSourceView, script shellcheckScript, fix *shellcheckFix, description string) []DiagnosticFix {
	if !source.Present() || !source.Literal() || fix == nil || len(fix.Replacements) == 0 {
		return nil
	}
	sanitized := strings.Split(sanitizeExpressionsInScript(source.Value()), "\n")
	edits := make([]DiagnosticEdit, 0, len(fix.Replacements))
	for _, replacement := range fix.Replacements {
		line := script.originalLine(replacement.Line)
		if replacement.Line != replacement.EndLine || line < 1 || line > source.LinesLen() ||
			strings.ContainsAny(replacement.Replacement, "\x00\r\n") || !utf8.ValidString(replacement.Replacement) {
			return nil
		}
		bounds, _ := source.Line(line - 1)
		original := source.Value()[bounds.Start:bounds.End]
		if original != sanitized[line-1] {
			return nil
		}
		startOffset, startOK := text.ByteOffsetAtColumn(original, replacement.Column)
		endOffset, endOK := text.ByteOffsetAtColumn(original, replacement.EndColumn)
		if !startOK || !endOK || startOffset > endOffset {
			return nil
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
			return nil
		}
		start := mapped.YamlColumn + utf8.RuneCountInString(source.Value()[mapped.Start:bounds.Start+startOffset])
		end := mapped.YamlColumn + utf8.RuneCountInString(source.Value()[mapped.Start:bounds.Start+endOffset])
		edits = append(edits, DiagnosticEdit{
			Start:       DiagnosticPosition{Line: mapped.YamlLine, Column: start},
			End:         DiagnosticPosition{Line: mapped.YamlLine, Column: end},
			Replacement: replacement.Replacement,
		})
	}
	slices.SortFunc(edits, func(a, b DiagnosticEdit) int {
		if a.Start.Line != b.Start.Line {
			return a.Start.Line - b.Start.Line
		}
		return a.Start.Column - b.Start.Column
	})
	for i, edit := range edits {
		if i > 0 {
			previous := edits[i-1]
			if previous.End.Line == edit.Start.Line && previous.End.Column >= edit.Start.Column {
				// Shared boundaries depend on ShellCheck's insertion precedence;
				// exposing them as unordered source edits would lose that meaning.
				return nil
			}
		}
	}
	if !shellcheckFixPreservesIndent(source, edits) {
		return nil
	}
	return []DiagnosticFix{{Description: description, Applicability: "suggested", Edits: edits}}
}

func shellcheckFixPreservesIndent(source documentparse.ScriptSourceView, edits []DiagnosticEdit) bool {
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
