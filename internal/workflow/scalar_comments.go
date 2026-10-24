package workflow

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/yaml"
)

type scalarCommentNodes struct {
	declaration int
	leading     []string
	trailing    []string
	block       bool
	scalar      *yaml.Node
}

// Preserve parser attachment before alias resolution changes the tree. Source
// lines only recover positions of comments already classified by the YAML parser.
func (p *Parser) indexScalarComments(root *yaml.Node) {
	p.scalarComments = make(map[Pos]scalarCommentNodes)
	p.scalarDeclarations = make(map[Pos]scalarCommentNodes)
	var visit func(*yaml.Node, []string)
	visit = func(node *yaml.Node, inherited []string) {
		if node == nil {
			return
		}
		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				leading := []string{key.HeadComment, value.HeadComment}
				if i == 0 {
					leading = append(leading, node.HeadComment)
					leading = append(leading, inherited...)
				} else {
					leading = append(leading, node.Content[i-2].FootComment, node.Content[i-1].FootComment)
				}
				if value.Kind == yaml.ScalarNode || value.Kind == yaml.AliasNode {
					attachment := scalarCommentNodes{
						declaration: key.Line,
						leading:     leading,
						trailing:    []string{key.LineComment, value.LineComment},
						block:       value.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0,
						scalar:      value,
					}
					p.scalarDeclarations[*PosAt(key)] = attachment
					if value.Kind == yaml.ScalarNode {
						p.scalarComments[*PosAt(value)] = attachment
					}
				}
				visit(value, leading)
			}
		case yaml.SequenceNode, yaml.DocumentNode:
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
		}
	}
	visit(root, nil)
}

// ScriptSourceForDeclaration keeps alias-use comments with their owning run
// declaration while text and fixes retain the anchored scalar's source spans.
func (p *Parser) ScriptSourceForDeclaration(node *yaml.Node, declaration *Pos) *ScriptSource {
	source := p.ScriptSource(node)
	if source == nil || declaration == nil {
		return source
	}
	attachment, exists := p.scalarDeclarations[*declaration]
	if !exists || attachment.scalar.Kind != yaml.AliasNode {
		return source
	}
	alias := attachment.scalar
	physical := &ScriptSource{
		ContentStart: Pos{alias.Line, alias.Column},
		ScalarEnd:    Pos{alias.Line, alias.Column + utf8.RuneCountInString(alias.Value) + 1},
	}
	declared := *source
	declared.Comments = attachment.comments(p.SourceLines, physical)
	return &declared
}

func (nodes scalarCommentNodes) comments(lines []string, source *ScriptSource) []ScalarComment {
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
	var comments []ScalarComment
	leading := parts(nodes.leading)
	for line := nodes.declaration - 1; line > 0 && line <= len(lines); line-- {
		text := strings.TrimSpace(lines[line-1])
		if !strings.HasPrefix(text, "#") || !slices.Contains(leading, text) {
			break
		}
		column := strings.Index(lines[line-1], "#")
		comments = append(comments, ScalarComment{Text: text, Pos: Pos{line, utf8.RuneCountInString(lines[line-1][:column]) + 1}})
	}
	slices.Reverse(comments)
	for _, text := range parts(nodes.trailing) {
		for line := max(1, nodes.declaration); line <= min(len(lines), source.ScalarEnd.Line); line++ {
			physical := lines[line-1]
			if !strings.HasSuffix(strings.TrimSpace(physical), text) {
				continue
			}
			prefix, _, found := strings.CutLast(physical, text)
			if !found {
				continue
			}
			pos := Pos{line, utf8.RuneCountInString(prefix) + 1}
			// Trailing comments lie before block/quoted content or after its closing token.
			// Matching text inside a scalar is not another attachment.
			before := line < source.ContentStart.Line || line == source.ContentStart.Line && (nodes.block || pos.Col < source.ContentStart.Col)
			after := line > source.ScalarEnd.Line || line == source.ScalarEnd.Line && pos.Col >= source.ScalarEnd.Col
			if before || after {
				comments = append(comments, ScalarComment{Text: text, Pos: pos, Trailing: true})
				break
			}
		}
	}
	slices.SortFunc(comments, func(a, b ScalarComment) int {
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line - b.Pos.Line
		}
		return a.Pos.Col - b.Pos.Col
	})
	return slices.CompactFunc(comments, func(a, b ScalarComment) bool { return a.Pos == b.Pos })
}
