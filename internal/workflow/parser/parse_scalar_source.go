package parser

import (
	"slices"
	"strings"
	"unicode/utf8"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// IndexScalarSources binds physical node identity and context before projection.
// Standalone ScriptSource calls without indexing use an independently parsed graph.
// Decode lazily: most workflow scalars never need source-level diagnostics.
func (p *Parser) IndexScalarSources(root *syntax.Node) {
	p.scalarContexts = make(map[workflowlang.Pos]sourcemap.Context)
	p.scalarSources = make(map[workflowlang.Pos]*workflowlang.ScriptSource)
	p.indexScalarComments(root)
	var visit func(*syntax.Node, sourcemap.Context)
	visit = func(n *syntax.Node, context sourcemap.Context) {
		if n == nil {
			return
		}
		if n.Kind == syntax.ScalarNode {
			p.scalarContexts[*PosAt(n)] = context
		}
		if n.Kind == syntax.MappingNode || n.Kind == syntax.SequenceNode {
			context.Flow = context.Flow || n.Style&syntax.FlowStyle != 0
			if !context.Flow {
				context.Indent = scalarCollectionIndent(n, p.sourceLines)
			}
		}
		for _, child := range n.Content {
			visit(child, context)
		}
	}
	visit(root, sourcemap.Context{Indent: -1})
}

// Collection positions can point at properties preceding their first entry.
// Recover the structural indentation once for all scalar consumers.
func scalarCollectionIndent(node *syntax.Node, lines []string) int {
	indent := node.Column - 1
	if len(node.Content) == 0 {
		return indent
	}
	first := node.Content[0]
	if node.Kind == syntax.MappingNode {
		if node.Anchor != "" || node.Style&syntax.TaggedStyle != 0 {
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

// ScriptSource returns an owned mapping; callers cannot mutate the parser's cache.
func (p *Parser) ScriptSource(n *syntax.Node) *workflowlang.ScriptSource {
	if n == nil || n.Kind != 0 && n.Kind != syntax.ScalarNode {
		return nil
	}
	if p.scalarContexts == nil {
		// Standalone parser users can request a source map before parsing a tree.
		source := strings.Join(p.sourceLines, "\n")
		if p.scalarDocument != nil {
			source = scalarDocumentSource(p.scalarDocument)
		}
		root, err := codec.Parse([]byte(source))
		if err != nil {
			return nil
		}
		p.IndexScalarSources(root)
	}
	cached := p.scalarSource(*PosAt(n), n.Value)
	if cached == nil {
		return nil
	}
	owned := cloneScriptSource(*cached)
	return &owned
}

// scalarSource retains the decoder result inside the parser. Public source
// mappings are copied; parse-boundary metadata only reads its value fields.
func (p *Parser) scalarSource(key workflowlang.Pos, value string) *workflowlang.ScriptSource {
	if source, exists := p.scalarSources[key]; exists {
		if source != nil && source.Value != value {
			return nil
		}
		return source
	}
	p.scalarSources[key] = nil
	context, exists := p.scalarContexts[key]
	if !exists {
		return nil
	}
	decoded, err := p.scalarDocument.Decode(sourcemap.Position{Line: key.Line, Column: key.Col}, context)
	if err != nil || decoded.Value != value {
		return nil
	}
	source := workflowlang.NewScriptSource(decoded.Value)
	source.Literal = decoded.Literal
	source.ContentStart = workflowlang.Pos{Line: decoded.Start.Line, Col: decoded.Start.Column}
	source.ScalarEnd = workflowlang.Pos{Line: decoded.End.Line, Col: decoded.End.Column}
	source.Comments = p.scalarComments[key].comments(p.sourceLines, source)
	for _, span := range decoded.Spans {
		endColumn := 0
		if span.Width != 0 {
			endColumn = span.Column + span.Width
		}
		endLine := 0
		if span.LineBreak {
			endLine, endColumn = span.Line+1, 1
		}
		source.Spans = append(source.Spans, workflowlang.ScriptSourceSpan{Start: span.Start, End: span.End, YamlLine: span.Line, YamlColumn: span.Column, YamlEndColumn: endColumn, YamlEndLine: endLine})
	}
	p.scalarSources[key] = source
	return source
}

// cloneScriptSource returns source with its slices detached. Copying the whole
// value keeps fields added later; a new reference field still shares storage
// until it is detached here.
func cloneScriptSource(source workflowlang.ScriptSource) workflowlang.ScriptSource {
	source.Lines = slices.Clone(source.Lines)
	source.Spans = slices.Clone(source.Spans)
	source.Comments = slices.Clone(source.Comments)
	return source
}

func scalarDocumentSource(document *sourcemap.Document) string {
	var source strings.Builder
	for i := range document.LineCount() {
		line, ending := document.Line(i)
		source.WriteString(line)
		source.WriteString(ending)
	}
	return source.String()
}
