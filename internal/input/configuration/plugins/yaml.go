package plugins

import (
	"bytes"
	"slices"

	"github.com/kjanat/go-yaml/v4"
)

func editYAMLPlugins(original []byte, root, sequence *yaml.Node) ([]byte, error) {
	if root.Style&yaml.FlowStyle != 0 {
		copyRoot := *root
		copyRoot.Content = slices.Clone(root.Content)
		root = &copyRoot
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == "plugins" {
				root.Content[i+1] = sequence
				return yaml.Marshal(root)
			}
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugins"}, sequence)
		return yaml.Marshal(root)
	}
	replacement := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugins"}, sequence,
	}}
	start, end := len(original), len(original)
	lines := bytes.SplitAfter(original, []byte{'\n'})
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "plugins" {
			continue
		}
		replacement.Content[0].LineComment = key.LineComment
		sequence.LineComment = value.LineComment
		start = manifestLineOffset(lines, key.Line)
		if i+2 < len(root.Content) {
			end = manifestLineOffset(lines, root.Content[i+2].Line)
		}
		// Keep comments/blank lines introducing the next setting or footer.
		for end > start {
			lineStart := bytes.LastIndexByte(original[:max(start, end-1)], '\n') + 1
			line := bytes.TrimSpace(original[lineStart:end])
			if len(line) != 0 && line[0] != '#' && !isManifestDocumentEnd(original[lineStart:end]) {
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
	if bytes.Contains(original, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	}
	if start == len(original) {
		// Append before an explicit document-end marker.
		for i, line := range lines {
			if isManifestDocumentEnd(line) {
				start = manifestLineOffset(lines, i+1)
				end = start
				break
			}
		}
		if start > 0 && original[start-1] != '\n' {
			data = append([]byte{'\n'}, data...)
		}
	}
	return spliceManifest(original, start, end, data), nil
}

func isManifestDocumentEnd(line []byte) bool {
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

func manifestLineOffset(lines [][]byte, line int) int {
	offset := 0
	for i := 0; i < line-1 && i < len(lines); i++ {
		offset += len(lines[i])
	}
	return offset
}
