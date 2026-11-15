package parser

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
)

// IndexScalarSources binds physical node identity and context before projection.
// Standalone ScriptSource calls without indexing use an independently parsed graph.
// Decode lazily: most workflow scalars never need source-level diagnostics.
func (p *Parser) IndexScalarSources(root *codec.Node) {
	p.scalarContexts = make(map[source.Position]sourcemap.Context)
	p.scalarSources = make(map[source.Position]*workflowlang.ScriptSource)
	p.indexScalarComments(root)
	var visit func(*codec.Node, sourcemap.Context)
	visit = func(n *codec.Node, context sourcemap.Context) {
		if n == nil {
			return
		}
		if n.Kind == codec.ScalarNode {
			p.scalarContexts[*PosAt(n)] = context
		}
		if n.Kind == codec.MappingNode || n.Kind == codec.SequenceNode {
			context.Flow = context.Flow || n.Style&codec.FlowStyle != 0
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
func scalarCollectionIndent(node *codec.Node, lines []string) int {
	indent := node.Column - 1
	if len(node.Content) == 0 {
		return indent
	}
	first := node.Content[0]
	if node.Kind == codec.MappingNode {
		if node.Anchor != "" || node.Style&codec.TaggedStyle != 0 {
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
func (p *Parser) ScriptSource(n *codec.Node) *workflowlang.ScriptSource {
	if n == nil || n.Kind != 0 && n.Kind != codec.ScalarNode {
		return nil
	}
	if p.scalarContexts == nil {
		// Standalone parser users can request a source map before parsing a tree.
		scriptSource := strings.Join(p.sourceLines, "\n")
		if p.scalarDocument != nil {
			scriptSource = scalarDocumentSource(p.scalarDocument)
		}
		root, err := codec.Parse([]byte(scriptSource))
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
func (p *Parser) scalarSource(key source.Position, value string) *workflowlang.ScriptSource {
	if scriptSource, exists := p.scalarSources[key]; exists {
		if scriptSource != nil && scriptSource.Value != value {
			return nil
		}
		return scriptSource
	}
	p.scalarSources[key] = nil
	context, exists := p.scalarContexts[key]
	if !exists {
		return nil
	}
	decoded, err := p.scalarDocument.Decode(key, context)
	if err != nil || decoded.Value != value {
		return nil
	}
	scriptSource := workflowlang.NewScriptSource(decoded.Value)
	scriptSource.Literal = decoded.Literal
	scriptSource.ContentStart = decoded.Start
	scriptSource.ScalarEnd = decoded.End
	scriptSource.Comments = p.scalarComments[key].comments(p.sourceLines, scriptSource)
	for _, span := range decoded.Spans {
		endColumn := 0
		if span.Width != 0 {
			endColumn = span.Column + span.Width
		}
		endLine := 0
		if span.LineBreak {
			endLine, endColumn = span.Line+1, 1
		}
		scriptSource.Spans = append(scriptSource.Spans, workflowlang.ScriptSourceSpan{Start: span.Start, End: span.End, YamlLine: span.Line, YamlColumn: span.Column, YamlEndColumn: endColumn, YamlEndLine: endLine})
	}
	p.scalarSources[key] = scriptSource
	return scriptSource
}

// cloneScriptSource returns source with its slices detached. Copying the whole
// value keeps fields added later; a new reference field still shares storage
// until it is detached here.
func cloneScriptSource(scriptSource workflowlang.ScriptSource) workflowlang.ScriptSource {
	scriptSource.Lines = slices.Clone(scriptSource.Lines)
	scriptSource.Spans = slices.Clone(scriptSource.Spans)
	scriptSource.Comments = slices.Clone(scriptSource.Comments)
	return scriptSource
}

func scalarDocumentSource(document *sourcemap.Document) string {
	var scriptSource strings.Builder
	for i := range document.LineCount() {
		line, ending := document.Line(i)
		scriptSource.WriteString(line)
		scriptSource.WriteString(ending)
	}
	return scriptSource.String()
}
