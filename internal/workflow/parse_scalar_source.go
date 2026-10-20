package workflow

import (
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/yaml"
	"actionlint.kjanat.dev/v2/internal/yamlsource"
)

type scalarContext = yamlsource.Context

// IndexScalarSources records structural context before alias resolution changes the physical tree.
// Decode lazily: most workflow scalars never need source-level diagnostics.
func (p *Parser) IndexScalarSources(root *yaml.Node) {
	p.scalarContexts = make(map[Pos]scalarContext)
	p.scalarSources = make(map[Pos]*ScriptSource)
	var visit func(*yaml.Node, scalarContext)
	visit = func(n *yaml.Node, context scalarContext) {
		if n == nil {
			return
		}
		if n.Kind == yaml.ScalarNode {
			p.scalarContexts[*PosAt(n)] = context
		}
		if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
			context.Flow = context.Flow || n.Style&yaml.FlowStyle != 0
			if !context.Flow {
				context.Indent = scalarCollectionIndent(n, p.SourceLines)
			}
		}
		for _, child := range n.Content {
			visit(child, context)
		}
	}
	visit(root, scalarContext{Indent: -1})
}

// Collection positions can point at properties preceding their first entry.
// Recover the structural indentation once for all scalar consumers.
func scalarCollectionIndent(node *yaml.Node, lines []string) int {
	indent := node.Column - 1
	if len(node.Content) == 0 {
		return indent
	}
	first := node.Content[0]
	if node.Kind == yaml.MappingNode {
		if node.Anchor != "" || node.Style&yaml.TaggedStyle != 0 {
			indent = first.Column - 1
			if len(node.Content) > 1 && node.Content[1].Line > first.Line {
				declaration := lines[node.Content[1].Line-1]
				prefix := strings.TrimLeft(declaration, " ")
				if prefix == ":" || strings.HasPrefix(prefix, ": ") || strings.HasPrefix(prefix, ":\t") {
					indent = len(declaration) - len(prefix)
				}
			}
		}
	} else if first.Line != node.Line {
		for line := node.Line; line <= first.Line; line++ {
			declaration := []rune(lines[line-1])
			start := 0
			if line == node.Line {
				start = node.Column - 1
			}
			prefix := strings.TrimLeft(string(declaration[start:]), " ")
			if prefix == "-" || strings.HasPrefix(prefix, "- ") || strings.HasPrefix(prefix, "-\t") {
				return len(declaration) - utf8.RuneCountInString(prefix)
			}
		}
	}
	return indent
}

func (p *Parser) ScriptSource(n *yaml.Node) *ScriptSource {
	if n == nil || n.Kind != 0 && n.Kind != yaml.ScalarNode {
		return nil
	}
	if p.scalarContexts == nil {
		// Standalone parser users can request a source map before parsing a tree.
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(strings.Join(p.SourceLines, "\n")), &root); err != nil {
			return nil
		}
		p.IndexScalarSources(&root)
	}
	key := *PosAt(n)
	if source, exists := p.scalarSources[key]; exists {
		if source != nil && source.Value != n.Value {
			return nil
		}
		return source
	}
	p.scalarSources[key] = nil
	context, exists := p.scalarContexts[key]
	if !exists {
		return nil
	}
	decoded, err := p.ScalarDocument.Decode(yamlsource.Position{Line: n.Line, Column: n.Column}, context)
	if err != nil || decoded.Value != n.Value {
		return nil
	}
	source := newScriptSource(decoded.Value)
	source.Literal = decoded.Literal
	source.ContentStart = Pos{Line: decoded.Start.Line, Col: decoded.Start.Column}
	source.ScalarEnd = Pos{Line: decoded.End.Line, Col: decoded.End.Column}
	for _, span := range decoded.Spans {
		endColumn := 0
		if span.Width != 0 {
			endColumn = span.Column + span.Width
		}
		endLine := 0
		if span.LineBreak {
			endLine, endColumn = span.Line+1, 1
		}
		source.Spans = append(source.Spans, ScriptSourceSpan{Start: span.Start, End: span.End, YamlLine: span.Line, YamlColumn: span.Column, YamlEndColumn: endColumn, YamlEndLine: endLine})
	}
	p.scalarSources[key] = source
	return source
}
