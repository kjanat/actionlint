package suppression

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	sourceposition "actionlint.kjanat.dev/v2/internal/source/position"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	rulemodel "actionlint.kjanat.dev/v2/rule"
	sourcemodel "actionlint.kjanat.dev/v2/source"
)

// Edit proposes a suppression for a physical YAML line. The caller must check
// policy and bind the returned edit to the captured source revision and path.
// Existing directives retain their selectors and reason; scalar content is
// targeted through its YAML declaration, never by inserting script comments.
func Edit(content []byte, line int, rule string, allowed []string, names rulemodel.Names) (diagnostic.Edit, bool) {
	if !slices.Contains(allowed, rule) {
		return diagnostic.Edit{}, false
	}
	document := sourcemap.NewDocument(string(content))
	if line < 1 || line > document.LineCount() {
		return diagnostic.Edit{}, false
	}
	directives, invalid := Parse(content, allowed, names)
	for _, directive := range directives {
		if line < directive.First || line > directive.Last {
			continue
		}
		if slices.Contains(directive.Rules, rule) {
			return diagnostic.Edit{}, false
		}
		text, _ := document.Line(directive.Position.Line - 1)
		rules := append(slices.Clone(directive.Rules), rule)
		replacement := "# ignore[" + strings.Join(rules, ", ") + "]"
		if directive.Position.Line < directive.First {
			replacement = "# actionlint-ignore-next-line " + strings.Join(rules, ", ")
		}
		if directive.Reason != "" {
			replacement += " -- " + directive.Reason
		}
		edit := diagnostic.Edit{Start: directive.Position, End: diagnostic.Position{Line: directive.Position.Line, Column: utf8.RuneCountInString(text) + 1}, Replacement: replacement}
		return validateEdit(content, edit, line, rule, allowed, invalid, 0, names)
	}
	target := line
	if root, err := codec.Parse(content); err == nil {
		parser := workflowparser.NewSourceParser(content)
		parser.IndexScalarSources(root)
		var visit func(*syntax.Node)
		visit = func(node *syntax.Node) {
			if node.Kind == syntax.ScalarNode && node.Line <= line {
				if scalar := parser.ScriptSource(node); scalar != nil && line <= scalar.ScalarEnd.Line {
					target = min(target, node.Line)
				}
			}
			for _, child := range node.Content {
				visit(child)
			}
		}
		visit(root)
	}
	text, ending := document.Line(target - 1)
	if ending == "" {
		ending = "\n"
		if target > 1 {
			_, ending = document.Line(target - 2)
		}
	}
	indent := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
	point := diagnostic.Position{Line: target, Column: 1}
	edit := diagnostic.Edit{Start: point, End: point, Replacement: indent + "# actionlint-ignore-next-line " + rule + ending}
	return validateEdit(content, edit, line, rule, allowed, invalid, 1, names)
}

// Validate through the suppression parser itself, including malformed YAML
// recovery. An edit must suppress its target without hiding grammar errors.
func validateEdit(content []byte, edit diagnostic.Edit, line int, rule string, allowed []string, previous []diagnostic.Diagnostic, shift int, names rulemodel.Names) (diagnostic.Edit, bool) {
	first, last, err := sourceposition.Range(content, sourcemodel.YAML, edit.Start, edit.End)
	if err != nil {
		return diagnostic.Edit{}, false
	}
	updated := string(content[:first]) + edit.Replacement + string(content[last:])
	directives, invalid := Parse([]byte(updated), allowed, names)
	if len(invalid) != len(previous) {
		return diagnostic.Edit{}, false
	}
	for i := range invalid {
		if invalid[i].Message != previous[i].Message {
			return diagnostic.Edit{}, false
		}
	}
	evaluation, _ := Evaluate(directives, nil, names)
	return edit, evaluation.Suppresses(rule, line+shift, line+shift)
}
