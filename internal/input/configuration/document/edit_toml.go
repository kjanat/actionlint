package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// setTOMLList keeps existing [[member]] tables and adds a missing member as a
// root assignment after the #:schema preamble.
func setTOMLList(original []byte, member string, sequence []any, fields TableFields) ([]byte, error) {
	var values []string
	for _, entry := range sequence {
		switch value := entry.(type) {
		case string:
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			values = append(values, string(encoded))
		case map[string]any:
			inline, err := tomlFields(entry, fields)
			if err != nil {
				return nil, err
			}
			values = append(values, "{ "+strings.Join(inline, ", ")+" }")
		default:
			return nil, fmt.Errorf("invalid %s entry", member)
		}
	}

	replacement := []byte(member + " = [" + strings.Join(values, ", ") + "]")
	parser := unstable.Parser{}
	parser.Reset(original)
	root := true
	pluginSection := -1
	var sections [][2]int
	for parser.NextExpression() {
		expression := parser.Expression()
		if expression.Kind == unstable.Table || expression.Kind == unstable.ArrayTable {
			root = false
			keys := expression.Key()
			if !keys.Next() {
				return nil, errors.New("TOML table has no key")
			}
			first := keys.Node()
			start := bytes.LastIndexByte(original[:int(first.Raw.Offset)], '\n') + 1
			if pluginSection >= 0 {
				sections = append(sections, [2]int{pluginSection, start})
				pluginSection = -1
			}
			if string(first.Data) == member {
				pluginSection = start
			}
		}
		if !root || expression.Kind != unstable.KeyValue {
			continue
		}
		keys := expression.Key()
		if keys.Next() && string(keys.Node().Data) == member && !keys.Next() {
			start := int(expression.Raw.Offset)
			return splice(original, start, start+int(expression.Raw.Length), replacement), nil
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	if pluginSection >= 0 {
		sections = append(sections, [2]int{pluginSection, len(original)})
	}
	for _, section := range slices.Backward(sections) {
		end := section[1]
		for end > section[0] {
			lineStart := bytes.LastIndexByte(original[:end-1], '\n') + 1
			line := bytes.TrimSpace(original[lineStart:end])
			if len(line) != 0 && line[0] != '#' {
				break
			}
			end = lineStart
		}
		original = splice(original, section[0], end, nil)
	}
	if len(sequence) != 0 {
		newline := string(lineEnding(original))
		tables, err := tomlTables(member, sequence, fields, newline)
		if err != nil {
			return nil, err
		}
		if len(sections) != 0 {
			return splice(original, sections[0][0], sections[0][0], tables), nil
		}
		// A new array of tables is a root declaration even after other tables.
		// Appending a root-style plugins assignment here would change its scope.
		result := slices.Clone(original)
		if len(result) != 0 && !bytes.HasSuffix(result, []byte(newline)) {
			result = append(result, newline...)
		}
		if len(result) != 0 && !bytes.HasSuffix(result, []byte(newline+newline)) {
			result = append(result, newline...)
		}
		return append(result, tables...), nil
	}
	// Root assignments precede tables. Keep the schema preamble first, while
	// comments following it remain attached to their original setting or table.
	position := 0
	for offset := 0; offset < len(original); {
		end := bytes.IndexByte(original[offset:], '\n')
		if end < 0 {
			end = len(original)
		} else {
			end += offset + 1
		}
		line := bytes.TrimSpace(original[offset:end])
		if len(line) != 0 && line[0] != '#' {
			break
		}
		if bytes.HasPrefix(line, []byte("#:schema ")) || bytes.HasPrefix(line, []byte("#:schema\t")) {
			position = end
		} else if len(line) == 0 && position == offset && position != 0 {
			position = end
		}
		offset = end
	}
	newline := lineEnding(original)
	if position > 0 && original[position-1] != '\n' {
		replacement = append(slices.Clone(newline), replacement...)
	}
	replacement = append(replacement, newline...)
	return splice(original, position, position, replacement), nil
}

func tomlFields(entry any, fields TableFields) ([]string, error) {
	if fields == nil {
		return nil, errors.New("TOML table entries need a field order")
	}
	pairs, err := fields(entry)
	if err != nil {
		return nil, err
	}
	rendered := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		value, err := json.Marshal(pair[1])
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, pair[0]+" = "+string(value))
	}
	return rendered, nil
}

func tomlTables(member string, entries []any, fields TableFields, newline string) ([]byte, error) {
	var tables []byte
	for index, entry := range entries {
		pairs, err := tomlFields(entry, fields)
		if err != nil {
			return nil, err
		}
		if index != 0 {
			tables = append(tables, newline...)
		}
		tables = append(tables, "[["+member+"]]"+newline...)
		for _, pair := range pairs {
			tables = append(tables, pair...)
			tables = append(tables, newline...)
		}
	}
	return tables, nil
}

type tomlSpan struct {
	start int
	end   int
}

type tomlExpression struct {
	kind     unstable.Kind
	path     []string
	start    int
	end      int
	comments map[int][]byte
}

// rewriteTOML moves comments of removed expressions above the new members.
func rewriteTOML(source []byte, value any, members []string) ([]byte, error) {
	values, _ := value.(map[string]any)
	canonical := make(map[string]any)
	for _, key := range members {
		if value, exists := values[key]; exists {
			canonical[key] = value
		}
	}
	generated, err := Marshal("actionlint.toml", canonical)
	if err != nil {
		return nil, err
	}

	parser := unstable.Parser{KeepComments: true}
	parser.Reset(source)
	var expressions []tomlExpression
	var table []string
	for parser.NextExpression() {
		node := parser.Expression()
		var path []string
		switch node.Kind {
		case unstable.Comment:
			path = slices.Clone(table)
		case unstable.Table, unstable.ArrayTable:
			path = tomlKeyPath(node)
			table = slices.Clone(path)
		case unstable.KeyValue:
			path = append(slices.Clone(table), tomlKeyPath(node)...)
		case unstable.Invalid, unstable.Key, unstable.Array, unstable.InlineTable, unstable.String, unstable.Bool, unstable.Float, unstable.Integer,
			unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
			// Value and meta kinds are not top-level expressions.
			fallthrough
		default:
			continue
		}
		start, end := tomlExpressionOffsets(node, len(source))
		expressions = append(expressions, tomlExpression{
			kind: node.Kind, path: path, start: start, end: end,
			comments: tomlExpressionComments(node, source),
		})
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}

	var removed []tomlSpan
	comments := map[int][]byte{}
	for i, expression := range expressions {
		if expression.kind == unstable.Comment {
			if following, ok := followingTOMLExpression(expressions, i, source); ok {
				if !isTOMLSchemaDirective(source, expression) && underMembers(expressions[following].path, members) {
					start, end := tomlLineSpan(source, expression.start, expression.end)
					removed = append(removed, tomlSpan{start: start, end: end})
					comments[expression.start] = bytes.Clone(source[expression.start:expression.end])
				}
				continue
			}
			if !underMembers(expression.path, members) {
				continue
			}
		} else if !underMembers(expression.path, members) || (expression.kind != unstable.KeyValue && expression.kind != unstable.Table && expression.kind != unstable.ArrayTable) {
			continue
		}
		start, end := tomlLineSpan(source, expression.start, expression.end)
		removed = append(removed, tomlSpan{start: start, end: end})
		maps.Copy(comments, expression.comments)
	}

	spans := make([]tomlSpan, 0, len(removed))
	for _, span := range removed {
		if span.start < span.end {
			spans = append(spans, span)
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	output := bytes.Clone(source)
	lastStart := len(source) + 1
	for _, span := range spans {
		if span.end > lastStart {
			return nil, errors.New("overlapping TOML migration source spans")
		}
		output = append(output[:span.start], output[span.end:]...)
		lastStart = span.start
	}

	commentOffsets := make([]int, 0, len(comments))
	for offset := range comments {
		commentOffsets = append(commentOffsets, offset)
	}
	sort.Ints(commentOffsets)
	newline := lineEnding(source)
	if bytes.Equal(newline, []byte("\r\n")) {
		generated = bytes.ReplaceAll(generated, []byte("\n"), newline)
	}
	if len(output) > 0 {
		if !bytes.HasSuffix(output, []byte("\n")) {
			output = append(output, newline...)
		}
		output = append(output, newline...)
	}
	if len(commentOffsets) > 0 {
		for i, offset := range commentOffsets {
			if i > 0 {
				output = append(output, newline...)
			}
			output = append(output, comments[offset]...)
		}
		output = append(output, newline...)
		output = append(output, newline...)
	}
	output = append(output, generated...)
	if !bytes.HasSuffix(output, []byte("\n")) {
		output = append(output, newline...)
	}
	return output, nil
}

func tomlKeyPath(node *unstable.Node) []string {
	var path []string
	for it := node.Key(); it.Next(); {
		path = append(path, string(it.Node().Data))
	}
	return path
}

func tomlExpressionOffsets(node *unstable.Node, sourceLength int) (int, int) {
	if node.Kind != unstable.Table && node.Kind != unstable.ArrayTable {
		return tomlRangeOffsets(node.Raw, sourceLength)
	}
	start, end := sourceLength, 0
	for it := node.Key(); it.Next(); {
		keyStart, keyEnd := tomlRangeOffsets(it.Node().Raw, sourceLength)
		if keyStart < start {
			start = keyStart
		}
		if keyEnd > end {
			end = keyEnd
		}
	}
	return start, end
}

func tomlRangeOffsets(raw unstable.Range, sourceLength int) (int, int) {
	start := int(raw.Offset)
	end := start + int(raw.Length)
	if start > sourceLength {
		start = sourceLength
	}
	if end > sourceLength {
		end = sourceLength
	}
	return start, end
}

func underMembers(path, members []string) bool {
	return len(path) > 0 && slices.Contains(members, path[0])
}

func tomlLineSpan(source []byte, start, end int) (int, int) {
	if start > len(source) {
		start = len(source)
	}
	if end > len(source) {
		end = len(source)
	}
	lineStart := bytes.LastIndexByte(source[:start], '\n') + 1
	lineEnd := bytes.IndexByte(source[end:], '\n')
	if lineEnd < 0 {
		lineEnd = len(source)
	} else {
		lineEnd += end
	}
	if lineEnd > lineStart && source[lineEnd-1] == '\r' {
		lineEnd--
	}
	return lineStart, lineEnd
}

func tomlExpressionComments(node *unstable.Node, source []byte) map[int][]byte {
	comments := map[int][]byte{}
	collectTOMLComments(node, source, comments)
	return comments
}

func collectTOMLComments(node *unstable.Node, source []byte, comments map[int][]byte) {
	for current := node; current != nil; current = current.Next() {
		if current.Kind == unstable.Comment {
			start, end := tomlRangeOffsets(current.Raw, len(source))
			comments[start] = bytes.Clone(source[start:end])
		}
		collectTOMLComments(current.Child(), source, comments)
	}
}

func followingTOMLExpression(expressions []tomlExpression, index int, source []byte) (int, bool) {
	end := expressions[index].end
	for next := index + 1; next < len(expressions); next++ {
		candidate := expressions[next]
		if candidate.kind == unstable.Comment {
			if !oneTOMLLineBreak(source[end:candidate.start]) {
				return 0, false
			}
			end = candidate.end
			continue
		}
		if candidate.kind != unstable.Table && candidate.kind != unstable.ArrayTable && candidate.kind != unstable.KeyValue {
			return 0, false
		}
		return next, oneTOMLLineBreak(source[end:candidate.start])
	}
	return 0, false
}

func isTOMLSchemaDirective(source []byte, expression tomlExpression) bool {
	return bytes.HasPrefix(bytes.TrimSpace(source[expression.start:expression.end]), []byte("#:schema"))
}

func oneTOMLLineBreak(trivia []byte) bool {
	return bytes.Count(trivia, []byte{'\n'}) == 1
}
