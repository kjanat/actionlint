package actionlint

import (
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

func (p *parser) workflowExpressionSources(root *yaml.Node) map[Pos]*scriptSource {
	sources := make(map[Pos]*scriptSource)
	seen := make(map[*yaml.Node]bool)
	var visit func(*yaml.Node, bool)
	visit = func(node *yaml.Node, condition bool) {
		if node == nil || seen[node] && node.Kind != yaml.ScalarNode {
			return
		}
		seen[node] = true
		if node.Kind == yaml.ScalarNode && (condition || strings.Contains(node.Value, "${{")) {
			if source := p.scriptSource(node); source != nil {
				sources[*posAt(node)] = source
			}
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				visit(node.Content[i], false)
				visit(node.Content[i+1], strings.EqualFold(node.Content[i].Value, "if"))
			}
			return
		}
		for _, child := range node.Content {
			visit(child, false)
		}
	}
	visit(root, false)
	return sources
}

func (rule *RuleExpression) scopeExpressionSource(value string, pos *Pos) func() {
	previous, offset := rule.expressionSource, rule.expressionOffset
	rule.expressionSource, rule.expressionOffset = nil, 0
	if rule.workflow != nil && pos != nil {
		if source := rule.workflow.expressionSources[*pos]; source != nil && source.value == value {
			rule.expressionSource = source
		}
	}
	return func() { rule.expressionSource, rule.expressionOffset = previous, offset }
}

func (rule *RuleExpression) expressionPosition(offset int, end bool) *Pos {
	source := rule.expressionSource
	if source == nil || offset < 0 || offset > len(source.value) {
		return nil
	}
	prefix := source.value[:offset]
	line, column := strings.Count(prefix, "\n")+1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:])+1
	if end || offset == len(source.value) {
		position, _ := source.endPos(line, column)
		return position
	}
	position, _ := source.pos(line, column)
	return position
}
