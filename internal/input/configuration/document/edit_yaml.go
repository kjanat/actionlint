package document

import (
	"bytes"
	"slices"
	"strings"

	"github.com/kjanat/go-yaml/v4"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

// setYAMLList replaces or appends the top-level member key. A flow-style root
// mapping is encoded again; otherwise only the member's lines change.
func setYAMLList(original []byte, key string, values []any) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(original, &document); err != nil {
		return nil, err
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if len(document.Content) > 0 {
		root = document.Content[0]
	}
	var sequence yaml.Node
	if err := sequence.Encode(values); err != nil {
		return nil, err
	}
	return setYAMLMember(original, root, key, &sequence)
}

func setYAMLMember(original []byte, root *yaml.Node, member string, sequence *yaml.Node) ([]byte, error) {
	if root.Style&yaml.FlowStyle != 0 {
		copyRoot := *root
		copyRoot.Content = slices.Clone(root.Content)
		root = &copyRoot
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == member {
				root.Content[i+1] = sequence
				return yaml.Marshal(root)
			}
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: member}, sequence)
		return yaml.Marshal(root)
	}
	replacement := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: member}, sequence,
	}}
	start, end := len(original), len(original)
	lines := bytes.SplitAfter(original, []byte{'\n'})
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != member {
			continue
		}
		replacement.Content[0].LineComment = key.LineComment
		sequence.LineComment = value.LineComment
		start = lineOffset(lines, key.Line)
		if i+2 < len(root.Content) {
			end = lineOffset(lines, root.Content[i+2].Line)
		}
		// Keep comments/blank lines introducing the next setting or footer.
		for end > start {
			lineStart := bytes.LastIndexByte(original[:max(start, end-1)], '\n') + 1
			line := bytes.TrimSpace(original[lineStart:end])
			if len(line) != 0 && line[0] != '#' && !isYAMLDocumentEnd(original[lineStart:end]) {
				break
			}
			end = lineStart
		}
		break
	}
	data, err := yaml.Marshal(replacement)
	if err != nil {
		return nil, err
	}
	data = bytes.ReplaceAll(data, []byte("\n"), lineEnding(original))
	if start == len(original) {
		// Append before an explicit document-end marker.
		for i, line := range lines {
			if isYAMLDocumentEnd(line) {
				start = lineOffset(lines, i+1)
				end = start
				break
			}
		}
		if start > 0 && original[start-1] != '\n' {
			data = append([]byte{'\n'}, data...)
		}
	}
	return splice(original, start, end, data), nil
}

// isYAMLDocumentEnd reports whether line is a "..." document end marker.
func isYAMLDocumentEnd(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	if !bytes.HasPrefix(line, []byte("...")) {
		return false
	}
	rest := line[3:]
	if len(rest) == 0 {
		return true
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return false
	}
	rest = bytes.TrimLeft(rest, " \t")
	return len(rest) == 0 || rest[0] == '#'
}

// lineOffset returns the byte offset of the 1-based line among lines.
func lineOffset(lines [][]byte, line int) int {
	offset := 0
	for i := 0; i < line-1 && i < len(lines); i++ {
		offset += len(lines[i])
	}
	return offset
}

// rewriteYAML encodes value and moves each comment of original to the setting
// that origins locates at the comment's key, or at its value when the setting
// has no key position.
func rewriteYAML(original []byte, value any, origins map[string]Location) ([]byte, error) {
	var source yaml.Node
	if err := yaml.Unmarshal(original, &source); err != nil {
		return nil, err
	}
	encoded, err := marshalValue(value, true)
	if err != nil {
		return nil, err
	}
	var target yaml.Node
	if err := target.Encode(encoded); err != nil {
		return nil, err
	}
	type pair struct{ key, value *yaml.Node }
	positions := map[sourcetext.Position]pair{}
	var collect func(*yaml.Node)
	collect = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				p := pair{key, val}
				positions[sourcetext.Position{Line: key.Line, Column: key.Column}] = p
				positions[sourcetext.Position{Line: val.Line, Column: val.Column}] = p
				collect(val)
			}
		} else {
			for _, child := range node.Content {
				collect(child)
			}
		}
	}
	collect(&source)
	used := map[*yaml.Node]bool{}
	copyComments := func(dst, src *yaml.Node) {
		if src == nil || used[src] {
			return
		}
		used[src] = true
		dst.HeadComment = src.HeadComment
		dst.LineComment = src.LineComment
		dst.FootComment = src.FootComment
	}
	var apply func(*yaml.Node, string)
	apply = func(node *yaml.Node, path string) {
		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				child := Path(path, key.Value)
				if origin, ok := origins[child]; ok {
					at := origin.Key
					if at.Line == 0 {
						at = origin.Value
					}
					p := positions[at]
					copyComments(key, p.key)
					copyComments(val, p.value)
					if val.LineComment != "" && (val.Kind == yaml.MappingNode || val.Kind == yaml.SequenceNode) {
						if key.LineComment == "" {
							key.LineComment = val.LineComment
						} else {
							key.HeadComment = strings.TrimSpace(key.HeadComment + "\n" + val.LineComment)
						}
						val.LineComment = ""
					}
				}
				apply(val, child)
			}
		case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode, yaml.StreamNode:
		case yaml.SequenceNode:
			for i, child := range node.Content {
				apply(child, Index(path, i))
			}
		}
	}
	apply(&target, "")
	var unattached []string
	var leftovers func(*yaml.Node)
	leftovers = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if !used[node] {
			for _, comment := range []string{node.HeadComment, node.LineComment, node.FootComment} {
				if comment != "" {
					unattached = append(unattached, comment)
				}
			}
		}
		for _, child := range node.Content {
			leftovers(child)
		}
	}
	leftovers(&source)
	if len(unattached) > 0 {
		target.HeadComment = strings.Join(append(unattached, target.HeadComment), "\n")
	}
	return yaml.Marshal(&target)
}
