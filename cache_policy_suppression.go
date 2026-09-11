package actionlint

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// filterCachePolicySuppressions reads exceptions from YAML comments. Script text
// and quoted strings remain values even when they contain directive-like text.
func filterCachePolicySuppressions(source []byte, errors []*Error) []*Error {
	if !bytes.Contains(source, []byte("actionlint:")) {
		return errors
	}
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return errors
	}
	lines := splitSourceLines(source)
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
		for _, name := range names {
			suppressed[target][name] = true
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
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Line > 0 && node.Line <= len(lines) {
			comment := strings.TrimSpace(node.LineComment)
			if comment != "" && strings.HasSuffix(strings.TrimSpace(lines[node.Line-1]), comment) {
				readComment(node.Line, comment, false)
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
			visit(child)
		}
	}
	visit(&root)
	filtered := make([]*Error, 0, len(errors)+len(directiveErrors))
	for _, err := range errors {
		if err.source == nil && isCachePolicy(err.Kind) && suppressed[err.Line][err.Kind] {
			continue
		}
		filtered = append(filtered, err)
	}
	return append(filtered, directiveErrors...)
}
