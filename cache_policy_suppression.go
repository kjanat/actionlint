package actionlint

import (
	"bytes"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// filterCachePolicySuppressions reads exceptions from YAML comments. Script text
// and quoted strings remain values even when they contain directive-like text.
func filterCachePolicySuppressions(source []byte, errors []*Error, policy *SuppressionsPolicy) []*Error {
	if !bytes.Contains(source, []byte("actionlint:")) {
		return errors
	}
	var root yaml.Node
	// Normalizing line endings keeps YAML comment attachment consistent while
	// preserving every source line and column used by diagnostics.
	if err := yaml.Unmarshal(bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n")), &root); err != nil {
		return errors
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
	suppressed := map[int]map[string]bool{}
	seen := map[int]bool{}
	var directiveErrors []*Error
	readComment := func(line int, comment string, standalone bool) {
		text := strings.TrimSpace(strings.TrimPrefix(comment, "#"))
		if !strings.HasPrefix(text, "actionlint:") || seen[line] {
			return
		}
		seen[line] = true
		col := strings.LastIndex(lines[line-1], comment)
		if col < 0 {
			return
		}
		pos := &Pos{Line: line, Col: utf8.RuneCountInString(lines[line-1][:col]) + 1}
		report := func(message string) {
			directiveErrors = append(directiveErrors, errorAt(pos, "inline-suppression", message))
		}
		declaration, reason, hasReason := strings.Cut(text, " -- ")
		command, selectors, _ := strings.Cut(declaration, " ")
		target := line
		switch command {
		case "actionlint:ignore":
			if standalone {
				report("use \"actionlint:ignore-next-line\" in a comment before the declaration")
				return
			}
		case "actionlint:ignore-next-line":
			if !standalone {
				report("\"actionlint:ignore-next-line\" must be on its own line immediately before the declaration")
				return
			}
			target++
		default:
			report("unknown inline suppression directive. use \"actionlint:ignore RULE -- reason\" or \"actionlint:ignore-next-line RULE -- reason\"")
			return
		}
		if !hasReason || strings.TrimSpace(reason) == "" {
			report("inline suppression requires a reason after \" -- \"")
			return
		}
		names := strings.Split(selectors, ",")
		for i, name := range names {
			name = strings.TrimSpace(name)
			if !isCachePolicy(name) {
				directiveErrors = append(directiveErrors, errorfAt(pos, "inline-suppression", "unknown cache policy rule %q. expected \"cache-write-untrusted\", \"cache-call-unrestricted\", or \"cache-operation\"", name))
				return
			}
			names[i] = name
		}
		if suppressed[target] == nil {
			suppressed[target] = map[string]bool{}
		}
		var prohibited []string
		for _, name := range names {
			mode := policy.reportFor(name)
			if mode == reportSuppression || mode == reportAll {
				if !slices.Contains(prohibited, name) {
					prohibited = append(prohibited, name)
				}
			}
			if mode == suppressionsAllowed || mode == reportSuppression {
				suppressed[target][name] = true
			}
		}
		if len(prohibited) != 0 {
			directiveErrors = append(directiveErrors, errorfAt(pos, "disallow-suppressions", "inline suppression of %s is disallowed by policy.disallow-suppressions", strings.Join(prohibited, ", ")))
		}
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
			if node.Style&yaml.FlowStyle != 0 {
				readCachePolicyFlowOpeningComments(node, lines, endLine, readComment)
			}
			comment := strings.TrimSpace(node.LineComment)
			if comment != "" {
				line := node.Line
				if node.Style&(yaml.FlowStyle|yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
					line = cachePolicyClosingCommentLine(node, comment, lines, endLine)
				}
				if line > 0 && strings.HasSuffix(strings.TrimSpace(lines[line-1]), comment) {
					readComment(line, comment, false)
					if line > node.Line {
						endLine = line - 1
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
	filtered := make([]*Error, 0, len(errors)+len(directiveErrors))
	for _, err := range errors {
		if err.source == nil && isCachePolicy(err.Kind) && suppressed[err.Line][err.Kind] {
			continue
		}
		filtered = append(filtered, err)
	}
	return append(filtered, directiveErrors...)
}

// cachePolicyClosingCommentLine locates a comment stored on a node's opening
// position even when its flow collection or quoted scalar spans multiple lines.
func cachePolicyClosingCommentLine(node *yaml.Node, comment string, lines []string, endLine int) int {
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

// readCachePolicyFlowOpeningComments reads comments at a parsed flow opener and
// its optional tag or anchor. YAML overwrites them with the closing comment.
func readCachePolicyFlowOpeningComments(node *yaml.Node, lines []string, endLine int, read func(int, string, bool)) {
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
				if next != "" && strings.ContainsAny(next[:1], "{[&!") {
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
		default:
			return
		}
	}
}
