package suppression

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	sourceposition "actionlint.kjanat.dev/v2/internal/input/source/position"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Remove deletes selected rule names from a validated YAML suppression. It
// preserves the directive's spelling, remaining separators and reason. Removing
// the last selector removes the comment, never YAML or embedded script content.
// The caller binds the returned edit to its captured source path and revision.
func Remove(content []byte, position sourcepos.Position, rules, allowed []string, names rule.Names) (results.Edit, bool) {
	if len(rules) == 0 {
		return results.Edit{}, false
	}
	directives, _ := Parse(content, allowed, names)
	var declaration *Directive
	for i := range directives {
		if directives[i].Position == position {
			declaration = &directives[i]
			break
		}
	}
	if declaration == nil {
		return results.Edit{}, false
	}
	for _, name := range rules {
		if !slices.Contains(declaration.Rules, name) {
			return results.Edit{}, false
		}
	}
	document := sourcemap.NewDocument(string(content))
	line, ending := document.Line(position.Line - 1)
	column, ok := document.ByteOffset(position.Line-1, position.Column)
	if !ok {
		return results.Edit{}, false
	}
	comment := line[column:]
	end := sourcepos.Position{Line: position.Line, Column: utf8.RuneCountInString(line) + 1}
	edit := results.Edit{Start: position, End: end}
	remaining := slices.DeleteFunc(slices.Clone(declaration.Rules), func(name string) bool { return slices.Contains(rules, name) })
	if len(remaining) == 0 {
		prefix := strings.TrimRight(line[:column], " \t")
		edit.Start.Column = utf8.RuneCountInString(prefix) + 1
		if prefix == "" && ending != "" {
			// Do not activate an earlier next-line directive by moving it
			// immediately above a declaration it did not previously cover.
			previous := ""
			if position.Line > 1 {
				previous, _ = document.Line(position.Line - 2)
			}
			previous = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(previous), "#"))
			if !isInlineSuppressionComment(previous) {
				edit.End = sourcepos.Position{Line: position.Line + 1, Column: 1}
			}
		}
	} else {
		declarationText, _, _ := strings.Cut(comment, " -- ")
		start := strings.Index(comment, strings.TrimSpace(strings.TrimPrefix(comment, "#")))
		selectorEnd := len(declarationText)
		selectorStart := start
		if strings.HasPrefix(comment[start:], "ignore[") {
			selectorStart += len("ignore[")
			selectorEnd = strings.LastIndex(declarationText, "]")
		} else {
			boundary := strings.IndexAny(comment[start:], " \t")
			selectorStart += boundary
			for selectorStart < selectorEnd && (comment[selectorStart] == ' ' || comment[selectorStart] == '\t') {
				selectorStart++
			}
		}
		if selectorStart < start || selectorEnd < selectorStart {
			return results.Edit{}, false
		}
		edit.Replacement = comment[:selectorStart] + removeSelectors(comment[selectorStart:selectorEnd], rules) + comment[selectorEnd:]
	}
	_, _, err := sourceposition.Range(content, sourcepos.YAML, edit.Start, edit.End)
	return edit, err == nil
}

// removeSelectors keeps separators from the original comment, including tabs
// and mixed comma/space forms. Tokens were validated by Parse before this call.
func removeSelectors(text string, removed []string) string {
	type token struct{ start, end int }
	var tokens []token
	start := -1
	for offset, char := range text {
		if char == ',' || unicode.IsSpace(char) {
			if start >= 0 {
				tokens = append(tokens, token{start, offset})
				start = -1
			}
		} else if start < 0 {
			start = offset
		}
	}
	if start >= 0 {
		tokens = append(tokens, token{start, len(text)})
	}
	var result strings.Builder
	retained := false
	for i, item := range tokens {
		if slices.Contains(removed, text[item.start:item.end]) {
			continue
		}
		if retained {
			result.WriteString(text[tokens[i-1].end:item.start])
		} else {
			result.WriteString(text[:tokens[0].start])
		}
		result.WriteString(text[item.start:item.end])
		retained = true
	}
	if len(tokens) > 0 {
		result.WriteString(text[tokens[len(tokens)-1].end:])
	}
	return result.String()
}
