package parser

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

type scalarCommentNodes struct {
	declaration int
	leading     []string
	trailing    []string
	block       bool
	scalar      *codec.Node
}

// Index physical comment attachments before building the parser projection. Source
// lines only recover positions of comments already classified by the YAML parser.
func (p *Parser) indexScalarComments(root *codec.Node) {
	p.scalarComments = make(map[source.Position]scalarCommentNodes)
	p.scalarDeclarations = make(map[source.Position]scalarCommentNodes)
	var visit func(*codec.Node, []string)
	visit = func(node *codec.Node, inherited []string) {
		if node == nil {
			return
		}
		switch node.Kind {
		case codec.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				leading := []string{key.HeadComment, value.HeadComment}
				if i == 0 {
					leading = append(leading, node.HeadComment)
					leading = append(leading, inherited...)
				} else {
					leading = append(leading, node.Content[i-2].FootComment, node.Content[i-1].FootComment)
				}
				if value.Kind == codec.ScalarNode || value.Kind == codec.AliasNode {
					attachment := scalarCommentNodes{
						declaration: key.Line,
						leading:     leading,
						trailing:    []string{key.LineComment, value.LineComment},
						block:       value.Style&(codec.LiteralStyle|codec.FoldedStyle) != 0,
						scalar:      value,
					}
					p.scalarDeclarations[*PosAt(key)] = attachment
					if value.Kind == codec.ScalarNode {
						p.scalarComments[*PosAt(value)] = attachment
					}
				}
				visit(value, leading)
			}
		case codec.SequenceNode, codec.DocumentNode:
			for i, child := range node.Content {
				leading := []string{child.HeadComment}
				if i == 0 {
					leading = append(leading, node.HeadComment)
					leading = append(leading, inherited...)
				} else {
					leading = append(leading, node.Content[i-1].FootComment)
				}
				visit(child, leading)
			}
		case codec.ScalarNode, codec.AliasNode:
		}
	}
	visit(root, nil)
}

// ScriptSourceForDeclaration attaches comments at the alias use to source spans
// from the anchored scalar.
func (p *Parser) ScriptSourceForDeclaration(node *codec.Node, declaration *source.Position) *workflowlang.ScriptSource {
	scriptSource := p.ScriptSource(node)
	if scriptSource == nil || declaration == nil {
		return scriptSource
	}
	attachment, exists := p.scalarDeclarations[*declaration]
	if !exists || attachment.scalar.Kind != codec.AliasNode {
		return scriptSource
	}
	alias := attachment.scalar
	physical := &workflowlang.ScriptSource{
		ContentStart: source.Position{Line: alias.Line, Column: alias.Column},
		ScalarEnd:    source.Position{Line: alias.Line, Column: alias.Column + utf8.RuneCountInString(alias.Value) + 1},
	}
	scriptSource.Comments = attachment.comments(p.sourceLines, physical)
	return scriptSource
}

func (nodes scalarCommentNodes) comments(lines []string, scriptSource *workflowlang.ScriptSource) []workflowlang.ScalarComment {
	parts := func(groups []string) []string {
		var comments []string
		for _, group := range groups {
			for part := range strings.SplitSeq(group, "\n") {
				if part = strings.TrimSpace(part); part != "" {
					comments = append(comments, part)
				}
			}
		}
		return comments
	}
	var comments []workflowlang.ScalarComment
	leading := parts(nodes.leading)
	for line := nodes.declaration - 1; line > 0 && line <= len(lines); line-- {
		text := strings.TrimSpace(lines[line-1])
		if !strings.HasPrefix(text, "#") || !slices.Contains(leading, text) {
			break
		}
		column := strings.Index(lines[line-1], "#")
		comments = append(comments, workflowlang.ScalarComment{Text: text, Pos: source.Position{Line: line, Column: utf8.RuneCountInString(lines[line-1][:column]) + 1}})
	}
	slices.Reverse(comments)
	for _, text := range parts(nodes.trailing) {
		for line := max(1, nodes.declaration); line <= min(len(lines), scriptSource.ScalarEnd.Line); line++ {
			physical := lines[line-1]
			if !strings.HasSuffix(strings.TrimSpace(physical), text) {
				continue
			}
			prefix, _, _ := strings.CutLast(physical, text)
			pos := source.Position{Line: line, Column: utf8.RuneCountInString(prefix) + 1}
			// Trailing comments lie before block/quoted content or after its closing token.
			// Matching text inside a scalar is not another attachment.
			before := line < scriptSource.ContentStart.Line || line == scriptSource.ContentStart.Line && (nodes.block || pos.Column < scriptSource.ContentStart.Column)
			after := line > scriptSource.ScalarEnd.Line || line == scriptSource.ScalarEnd.Line && pos.Column >= scriptSource.ScalarEnd.Column
			if before || after {
				comments = append(comments, workflowlang.ScalarComment{Text: text, Pos: pos, Trailing: true})
				break
			}
		}
	}
	slices.SortFunc(comments, func(a, b workflowlang.ScalarComment) int {
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line - b.Pos.Line
		}
		return a.Pos.Column - b.Pos.Column
	})
	return slices.CompactFunc(comments, func(a, b workflowlang.ScalarComment) bool { return a.Pos == b.Pos })
}
