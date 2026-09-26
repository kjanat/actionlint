package actionlint

import (
	"slices"
	"strings"
	"unicode/utf8"
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
func shellcheckDiagnosticFixes(source *scriptSource, script shellcheckScript, fix *shellcheckFix, description string) []DiagnosticFix {
	if source == nil || !source.literal || fix == nil || len(fix.Replacements) == 0 {
		return nil
	}
	sanitized := strings.Split(sanitizeExpressionsInScript(source.value), "\n")
	edits := make([]DiagnosticEdit, 0, len(fix.Replacements))
	for _, replacement := range fix.Replacements {
		line := script.originalLine(replacement.Line)
		if replacement.Line != replacement.EndLine || line < 1 || line > len(source.lines) ||
			strings.ContainsAny(replacement.Replacement, "\x00\r\n") || !utf8.ValidString(replacement.Replacement) {
			return nil
		}
		bounds := source.lines[line-1]
		original := source.value[bounds.start:bounds.end]
		if original != sanitized[line-1] {
			return nil
		}
		startOffset, startOK := byteOffsetAtColumn(original, replacement.Column)
		endOffset, endOK := byteOffsetAtColumn(original, replacement.EndColumn)
		if !startOK || !endOK || startOffset > endOffset {
			return nil
		}
		// Require both boundaries in the same exact span. Insertions at the end
		// of a line are valid even though they are not diagnostic start positions.
		var mapped *scriptSourceSpan
		for i := range source.spans {
			span := &source.spans[i]
			if bounds.start+startOffset >= span.start && bounds.start+endOffset <= span.end {
				mapped = span
				break
			}
		}
		if mapped == nil || mapped.start == mapped.end {
			return nil
		}
		start := mapped.yamlColumn + utf8.RuneCountInString(source.value[mapped.start:bounds.start+startOffset])
		end := mapped.yamlColumn + utf8.RuneCountInString(source.value[mapped.start:bounds.start+endOffset])
		edits = append(edits, DiagnosticEdit{
			Start:       DiagnosticPosition{Line: mapped.yamlLine, Column: start},
			End:         DiagnosticPosition{Line: mapped.yamlLine, Column: end},
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
	return []DiagnosticFix{{Description: description, Edits: edits}}
}

func shellcheckFixPreservesIndent(source *scriptSource, edits []DiagnosticEdit) bool {
	for _, span := range source.spans {
		original := source.value[span.start:span.end]
		modified := original
		for _, edit := range slices.Backward(edits) {
			if edit.Start.Line != span.yamlLine {
				continue
			}
			start, startOK := byteOffsetAtColumn(original, edit.Start.Column-span.yamlColumn+1)
			end, endOK := byteOffsetAtColumn(original, edit.End.Column-span.yamlColumn+1)
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
