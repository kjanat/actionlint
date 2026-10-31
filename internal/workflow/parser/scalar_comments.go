package parser

import (
	"slices"
	"strings"
	"unicode/utf8"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

type scalarCommentNodes struct {
	declaration int
	leading     []string
	trailing    []string
	block       bool
	scalar      *syntax.Node
}

// Index physical comment attachments before building the parser projection. Source
// lines only recover positions of comments already classified by the YAML parser.
func (p *Parser) indexScalarComments(root *syntax.Node) {
	p.scalarComments = make(map[workflowlang.Pos]scalarCommentNodes)
	p.scalarDeclarations = make(map[workflowlang.Pos]scalarCommentNodes)
	var visit func(*syntax.Node, []string)
	visit = func(node *syntax.Node, inherited []string) {
		if node == nil {
			return
		}
		switch node.Kind {
		case syntax.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				leading := []string{key.HeadComment, value.HeadComment}
				if i == 0 {
					leading = append(leading, node.HeadComment)
					leading = append(leading, inherited...)
				} else {
					leading = append(leading, node.Content[i-2].FootComment, node.Content[i-1].FootComment)
				}
				if value.Kind == syntax.ScalarNode || value.Kind == syntax.AliasNode {
					attachment := scalarCommentNodes{
						declaration: key.Line,
						leading:     leading,
						trailing:    []string{key.LineComment, value.LineComment},
						block:       value.Style&(syntax.LiteralStyle|syntax.FoldedStyle) != 0,
						scalar:      value,
					}
					p.scalarDeclarations[*PosAt(key)] = attachment
					if value.Kind == syntax.ScalarNode {
						p.scalarComments[*PosAt(value)] = attachment
					}
				}
				visit(value, leading)
			}
		case syntax.SequenceNode, syntax.DocumentNode:
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
		case syntax.ScalarNode, syntax.AliasNode:
		}
	}
	visit(root, nil)
}

// ScriptSourceForDeclaration attaches comments at the alias use to source spans
// from the anchored scalar.
func (p *Parser) ScriptSourceForDeclaration(node *syntax.Node, declaration *workflowlang.Pos) *workflowlang.ScriptSource {
	source := p.ScriptSource(node)
	if source == nil || declaration == nil {
		return source
	}
	attachment, exists := p.scalarDeclarations[*declaration]
	if !exists || attachment.scalar.Kind != syntax.AliasNode {
		return source
	}
	alias := attachment.scalar
	physical := &workflowlang.ScriptSource{
		ContentStart: workflowlang.Pos{Line: alias.Line, Col: alias.Column},
		ScalarEnd:    workflowlang.Pos{Line: alias.Line, Col: alias.Column + utf8.RuneCountInString(alias.Value) + 1},
	}
	source.Comments = attachment.comments(p.sourceLines, physical)
	return source
}

func (nodes scalarCommentNodes) comments(lines []string, source *workflowlang.ScriptSource) []workflowlang.ScalarComment {
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
		comments = append(comments, workflowlang.ScalarComment{Text: text, Pos: workflowlang.Pos{Line: line, Col: utf8.RuneCountInString(lines[line-1][:column]) + 1}})
	}
	slices.Reverse(comments)
	for _, text := range parts(nodes.trailing) {
		for line := max(1, nodes.declaration); line <= min(len(lines), source.ScalarEnd.Line); line++ {
			physical := lines[line-1]
			if !strings.HasSuffix(strings.TrimSpace(physical), text) {
				continue
			}
			prefix, _, _ := strings.CutLast(physical, text)
			pos := workflowlang.Pos{Line: line, Col: utf8.RuneCountInString(prefix) + 1}
			// Trailing comments lie before block/quoted content or after its closing token.
			// Matching text inside a scalar is not another attachment.
			before := line < source.ContentStart.Line || line == source.ContentStart.Line && (nodes.block || pos.Col < source.ContentStart.Col)
			after := line > source.ScalarEnd.Line || line == source.ScalarEnd.Line && pos.Col >= source.ScalarEnd.Col
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
		return a.Pos.Col - b.Pos.Col
	})
	return slices.CompactFunc(comments, func(a, b workflowlang.ScalarComment) bool { return a.Pos == b.Pos })
}
