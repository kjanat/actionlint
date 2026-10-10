package actionlint

import (
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// recoverInlineSuppressionComments reads lexical comments when malformed YAML
// prevents node construction. Quoted scalars and block bodies remain opaque.
func recoverInlineSuppressionComments(source []byte) []inlineSuppressionComment {
	lines := splitSourceLines(source)
	var comments []inlineSuppressionComment
	var quote byte
	var flow []int
	blockEnds := map[int]int{}
	parentIndent, blockEnd := 0, 0
	prefixPending := false
	for i, line := range lines {
		lineNumber := i + 1
		if lineNumber <= blockEnd {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		commentOrEmpty := trimmed == "" || strings.HasPrefix(trimmed, "#")
		marker := strings.HasPrefix(line, "---") || strings.HasPrefix(line, "...")
		if quote == 0 && marker && i > 0 && (len(line) == 3 || line[3] == ' ' || line[3] == '\t') {
			break
		}
		if !prefixPending || !commentOrEmpty && !strings.ContainsRune("|>!&", rune(trimmed[0])) {
			parentIndent = len(line) - len(trimmed)
		}
		keyIndent := parentIndent
		plain := false
		for col := 0; col < len(line); col++ {
			c := line[col]
			if quote != 0 {
				if quote == '"' && c == '\\' {
					col++
				} else if c == quote {
					if quote == '\'' && col+1 < len(line) && line[col+1] == '\'' {
						col++
					} else {
						quote = 0
						plain = true
					}
				}
				continue
			}
			separated := col == 0 || line[col-1] == ' ' || line[col-1] == '\t'
			if c == '#' && separated {
				text := strings.TrimSpace(line[col+1:])
				standalone := strings.TrimSpace(line[:col]) == ""
				if standalone {
					if i+1 >= len(lines) {
						break
					}
					next := strings.TrimSpace(lines[i+1])
					if next == "" || strings.HasPrefix(next, "#") {
						break
					}
				}
				if strings.HasPrefix(text, "actionlint:") {
					comments = append(comments, inlineSuppressionComment{
						pos: Pos{Line: lineNumber, Col: utf8.RuneCountInString(line[:col]) + 1}, text: text, standalone: standalone,
					})
				}
				break
			}
			if c == ' ' || c == '\t' {
				continue
			}
			nextSeparated := col+1 == len(line) || line[col+1] == ' ' || line[col+1] == '\t'
			switch {
			case c == ':' && (nextSeparated || len(flow) > 0):
				plain = false
				parentIndent = keyIndent
			case !plain && (c == '-' || c == '?') && nextSeparated:
				parentIndent = col
				keyIndent = col + 2
			case !plain && (c == '&' || c == '!'):
				for col+1 < len(line) && !strings.ContainsRune(" \t[{", rune(line[col+1])) {
					col++
				}
			case !plain && (c == '\'' || c == '"'):
				quote = c
			case !plain && (c == '[' || c == '{'):
				flow = append(flow, lineNumber)
			case len(flow) > 0 && (c == ']' || c == '}'):
				flow = flow[:len(flow)-1]
				plain = true
			case len(flow) > 0 && c == ',':
				plain = false
			case !plain && len(flow) == 0 && (c == '|' || c == '>'):
				node := &yaml.Node{Line: lineNumber, Column: utf8.RuneCountInString(line[:col]) + 1}
				readYAMLNodePrefixComments(node, lines, len(lines), parentIndent, func(int, string, bool) {}, blockEnds)
				blockEnd = blockEnds[lineNumber]
				plain = true
			default:
				plain = true
			}
		}
		if !commentOrEmpty {
			prefixPending = !plain && quote == 0 && len(flow) == 0
		}
	}
	for _, opening := range flow {
		// An unfinished flow collection can place its syntax diagnostic at EOF.
		// Keep that error within the opening declaration's suppression range.
		blockEnds[opening] = len(lines) + 1
	}
	for i := range comments {
		target := comments[i].pos.Line
		if comments[i].standalone {
			target++
		}
		comments[i].endLine = blockEnds[target]
	}
	return comments
}
