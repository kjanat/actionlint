package actionlint

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// inlineSuppressionComment is a real YAML comment attached to a declaration.
// Its position refers to the comment, independently of the YAML node's start.
type inlineSuppressionComment struct {
	pos        Pos
	text       string
	standalone bool
}

// collectInlineSuppressionComments isolates YAML attachment and source recovery
// from directive grammar and policy. Strings and script bodies are never comments.
func collectInlineSuppressionComments(source []byte) []inlineSuppressionComment {
	if !bytes.Contains(source, []byte("actionlint:")) {
		return nil
	}
	var root yaml.Node
	// Normalizing line endings keeps YAML comment attachment consistent while
	// preserving every source line and column used by diagnostics.
	if err := yaml.Unmarshal(bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n")), &root); err != nil {
		return nil
	}
	lines := splitSourceLines(source)
	documentEnd := len(lines)
	if len(root.Content) != 0 {
		// Unmarshal and Parse read the first document only. Block scalar content
		// is indented, so only column-one markers delimit this source range.
		for i := root.Content[0].Line; i < len(lines); i++ {
			line := lines[i]
			if (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "...")) && (len(line) == 3 || line[3] == ' ' || line[3] == '\t') {
				documentEnd = i
				break
			}
		}
	}
	var comments []inlineSuppressionComment
	seen := map[int]bool{}
	readComment := func(line int, comment string, standalone bool) {
		text := strings.TrimSpace(strings.TrimPrefix(comment, "#"))
		if !strings.HasPrefix(text, "actionlint:") || seen[line] {
			return
		}
		col := strings.LastIndex(lines[line-1], comment)
		if col < 0 {
			return
		}
		seen[line] = true
		comments = append(comments, inlineSuppressionComment{
			pos:        Pos{Line: line, Col: utf8.RuneCountInString(lines[line-1][:col]) + 1},
			text:       text,
			standalone: standalone,
		})
	}
	readPreceding := func(line int, comments string) {
		if line > 1 && line <= len(lines) && comments != "" {
			previous := strings.TrimSpace(lines[line-2])
			if strings.HasPrefix(previous, "#") && strings.HasSuffix(strings.TrimSpace(comments), previous) {
				readComment(line-1, previous, true)
			}
		}
	}
	var visit func(*yaml.Node, int)
	visit = func(node *yaml.Node, endLine int) {
		if node.Line > 0 && node.Line <= len(lines) {
			if node.Style&yaml.FlowStyle != 0 || node.Kind == yaml.ScalarNode {
				readYAMLNodePrefixComments(node, lines, endLine, readComment)
			}
			commentEnd := endLine
			// YAML can combine property and value comments into one string.
			// Locate each physical comment independently within the node's range.
			for part := range strings.SplitSeq(node.LineComment, "\n") {
				comment := strings.TrimSpace(part)
				if comment == "" {
					continue
				}
				line := node.Line
				if node.Style&yaml.FlowStyle != 0 || node.Kind == yaml.ScalarNode && node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
					line = yamlClosingCommentLine(node, comment, lines, commentEnd)
				}
				if line > 0 && strings.HasSuffix(strings.TrimSpace(lines[line-1]), comment) {
					readComment(line, comment, false)
					if node.Style&yaml.FlowStyle != 0 && line > node.Line {
						endLine = min(endLine, line-1)
					}
				}
			}
			readPreceding(node.Line, node.HeadComment)
		}
		for i, child := range node.Content {
			// The YAML parser can attach a standalone comment to the preceding
			// entry, including when CRLF changes comment attachment.
			if node.Kind == yaml.MappingNode && i >= 2 && i%2 == 0 {
				readPreceding(child.Line, node.Content[i-2].FootComment)
				readPreceding(child.Line, node.Content[i-1].FootComment)
			} else if node.Kind == yaml.SequenceNode && i > 0 {
				readPreceding(child.Line, node.Content[i-1].FootComment)
			}
			childEnd := endLine
			if i+1 < len(node.Content) {
				childEnd = min(childEnd, node.Content[i+1].Line-1)
			}
			visit(child, max(child.Line, childEnd))
		}
	}
	visit(&root, documentEnd)
	return comments
}

// yamlClosingCommentLine locates a comment stored on a node's opening
// position even when its flow collection or scalar spans multiple lines.
func yamlClosingCommentLine(node *yaml.Node, comment string, lines []string, endLine int) int {
	last := node
	for len(last.Content) > 0 {
		last = last.Content[len(last.Content)-1]
	}
	for line := endLine; line >= last.Line; line-- {
		text := strings.TrimSpace(lines[line-1])
		if strings.HasPrefix(text, "#") || !strings.HasSuffix(text, comment) {
			continue
		}
		prefix := strings.TrimSpace(strings.TrimSuffix(text, comment))
		prefix = strings.TrimSpace(strings.TrimSuffix(prefix, ","))
		if node.Kind == yaml.ScalarNode || strings.HasSuffix(prefix, "}") || strings.HasSuffix(prefix, "]") {
			return line
		}
	}
	return 0
}

// readYAMLNodePrefixComments reads comments on tags, anchors, flow openers and
// block scalar headers. Stop at value content, which can contain comment-like text.
// YAML may overwrite these prefix comments with the final value comment.
func readYAMLNodePrefixComments(node *yaml.Node, lines []string, endLine int, read func(int, string, bool)) {
	line := node.Line
	text := string([]rune(lines[line-1])[node.Column-1:])
	declaration := true
	for {
		text = strings.TrimLeft(text, " \t")
		switch {
		case text == "":
			if line >= endLine {
				return
			}
			line++
			text = lines[line-1]
			declaration = false
		case strings.HasPrefix(text, "#"):
			if declaration {
				read(line, text, false)
			} else if line < endLine {
				next := strings.TrimLeft(lines[line], " \t")
				if next != "" && !strings.HasPrefix(next, "#") {
					read(line, text, true)
				}
			}
			text = ""
		case strings.HasPrefix(text, "!<"):
			_, text, _ = strings.Cut(text, ">")
			declaration = true
		case strings.HasPrefix(text, "&"), strings.HasPrefix(text, "!"):
			end := strings.IndexAny(text, " \t{[")
			if end < 0 {
				text = ""
			} else {
				text = text[end:]
			}
			declaration = true
		case strings.HasPrefix(text, "{"), strings.HasPrefix(text, "["):
			if comment := strings.TrimLeft(text[1:], " \t"); strings.HasPrefix(comment, "#") {
				read(line, comment, false)
			}
			return
		case strings.HasPrefix(text, "|"), strings.HasPrefix(text, ">"):
			if end := strings.IndexAny(text, " \t"); end >= 0 {
				if comment := strings.TrimLeft(text[end:], " \t"); strings.HasPrefix(comment, "#") {
					read(line, comment, false)
				}
			}
			return
		default:
			return
		}
	}
}
