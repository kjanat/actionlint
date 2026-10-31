package config

import (
	"bytes"
	"errors"
	"maps"
	"slices"
	"sort"

	"github.com/kjanat/go-yaml/v4"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type tomlMigrationSpan struct {
	start int
	end   int
}

type tomlMigrationExpression struct {
	kind     unstable.Kind
	path     []string
	start    int
	end      int
	comments map[int][]byte
}

// migrateTOML rewrites legacy settings and the merged lint subtree using parser
// spans to distinguish comments from '#' characters in strings.
func migrateTOML(source []byte, migrated *yaml.Node) ([]byte, error) {
	if hasUnknownRepositoryFacts(migrated) {
		return nil, errors.New("TOML cannot preserve unknown values or null resets; convert the selected config to YAML or JSON first")
	}
	var values map[string]any
	if err := migrated.Decode(&values); err != nil {
		return nil, err
	}
	canonical := make(map[string]any)
	for _, key := range []string{"lint", "repository", "overrides"} {
		if value, exists := values[key]; exists {
			canonical[key] = value
		}
	}
	generated, err := toml.Marshal(canonical)
	if err != nil {
		return nil, err
	}

	parser := unstable.Parser{KeepComments: true}
	parser.Reset(source)
	var expressions []tomlMigrationExpression
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
		expressions = append(expressions, tomlMigrationExpression{
			kind: node.Kind, path: path, start: start, end: end,
			comments: tomlExpressionComments(node, source),
		})
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}

	var removed []tomlMigrationSpan
	comments := map[int][]byte{}
	for i, expression := range expressions {
		if expression.kind == unstable.Comment {
			if following, ok := followingTOMLMigrationExpression(expressions, i, source); ok {
				if !isTOMLSchemaDirective(source, expression) && isMigrationTOMLPath(expressions[following].path) {
					start, end := tomlLineSpan(source, expression.start, expression.end)
					removed = append(removed, tomlMigrationSpan{start: start, end: end})
					comments[expression.start] = bytes.Clone(source[expression.start:expression.end])
				}
				continue
			}
			if !isMigrationTOMLPath(expression.path) {
				continue
			}
		} else if !isMigrationTOMLPath(expression.path) || (expression.kind != unstable.KeyValue && expression.kind != unstable.Table && expression.kind != unstable.ArrayTable) {
			continue
		}
		start, end := tomlLineSpan(source, expression.start, expression.end)
		removed = append(removed, tomlMigrationSpan{start: start, end: end})
		maps.Copy(comments, expression.comments)
	}

	spans := make([]tomlMigrationSpan, 0, len(removed))
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
	newline := []byte("\n")
	if bytes.Contains(source, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
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

func containsNull(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.ShortTag() == "!!null" {
		return true
	}
	return slices.ContainsFunc(node.Content, containsNull)
}

func hasUnknownRepositoryFacts(node *yaml.Node) bool {
	if containsNull(field(node, "repository")) {
		return true
	}
	if overrides := field(node, "overrides"); overrides != nil {
		return slices.ContainsFunc(overrides.Content, hasUnknownRepositoryFacts)
	}
	return false
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
		return tomlMigrationOffsets(node.Raw, sourceLength)
	}
	start, end := sourceLength, 0
	for it := node.Key(); it.Next(); {
		keyStart, keyEnd := tomlMigrationOffsets(it.Node().Raw, sourceLength)
		if keyStart < start {
			start = keyStart
		}
		if keyEnd > end {
			end = keyEnd
		}
	}
	return start, end
}

func tomlMigrationOffsets(raw unstable.Range, sourceLength int) (int, int) {
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

func isMigrationTOMLPath(path []string) bool {
	return len(path) > 0 && (path[0] == "lint" || path[0] == "repository" || path[0] == "overrides" || slices.Contains(legacyKeys, path[0]))
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
			start, end := tomlMigrationOffsets(current.Raw, len(source))
			comments[start] = bytes.Clone(source[start:end])
		}
		collectTOMLComments(current.Child(), source, comments)
	}
}

func followingTOMLMigrationExpression(expressions []tomlMigrationExpression, index int, source []byte) (int, bool) {
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

func isTOMLSchemaDirective(source []byte, expression tomlMigrationExpression) bool {
	return bytes.HasPrefix(bytes.TrimSpace(source[expression.start:expression.end]), []byte("#:schema"))
}

func oneTOMLLineBreak(trivia []byte) bool {
	return bytes.Count(trivia, []byte{'\n'}) == 1
}
