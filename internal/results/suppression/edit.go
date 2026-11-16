package suppression

import (
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext/byteoffset"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Edit proposes a suppression for a physical YAML line. The caller must check
// policy and bind the returned edit to the captured source revision and path.
// Existing directives retain their selectors and reason; scalar content is
// targeted through its YAML declaration, never by inserting script comments.
func Edit(content []byte, line int, rule string, allowed []string, names rule.Names) (results.Edit, bool) {
	if !slices.Contains(allowed, rule) {
		return results.Edit{}, false
	}
	document := sourcemap.NewDocument(string(content))
	if line < 1 || line > document.LineCount() {
		return results.Edit{}, false
	}
	directives, invalid := ParseInlineSuppressions(content, allowed, names)
	for _, directive := range directives {
		if !directive.Lines.Contains(line) {
			continue
		}
		if slices.Contains(directive.Rules, rule) {
			return results.Edit{}, false
		}
		text, _ := document.Line(directive.Position.Line - 1)
		rules := append(slices.Clone(directive.Rules), rule)
		replacement := "# ignore[" + strings.Join(rules, ", ") + "]"
		if directive.Position.Line < directive.Lines.First {
			replacement = "# actionlint-ignore-next-line " + strings.Join(rules, ", ")
		}
		if directive.Reason != "" {
			replacement += " -- " + directive.Reason
		}
		edit := results.Edit{Start: directive.Position, End: sourcetext.Position{Line: directive.Position.Line, Column: utf8.RuneCountInString(text) + 1}, Replacement: replacement}
		return validateEdit(content, edit, line, rule, allowed, invalid, 0, names)
	}
	target := line
	if root, err := codec.ParseYAML(content); err == nil {
		parser := parser.NewSourceParser(content)
		parser.IndexScalarSources(root)
		var visit func(*codec.Node)
		visit = func(node *codec.Node) {
			if node.Kind == codec.ScalarNode && node.Line <= line {
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
	point := sourcetext.Position{Line: target, Column: 1}
	edit := results.Edit{Start: point, End: point, Replacement: indent + "# actionlint-ignore-next-line " + rule + ending}
	return validateEdit(content, edit, line, rule, allowed, invalid, 1, names)
}

// Validate through the suppression parser itself, including malformed YAML
// recovery. An edit must suppress its target without hiding grammar errors.
func validateEdit(content []byte, edit results.Edit, line int, rule string, allowed []string, previous []results.Diagnostic, shift int, names rule.Names) (results.Edit, bool) {
	first, last, err := byteoffset.Range(content, sourcetext.YAML, edit.Start, edit.End)
	if err != nil {
		return results.Edit{}, false
	}
	updated := string(content[:first]) + edit.Replacement + string(content[last:])
	directives, invalid := ParseInlineSuppressions([]byte(updated), allowed, names)
	if len(invalid) != len(previous) {
		return results.Edit{}, false
	}
	for i := range invalid {
		if invalid[i].Message != previous[i].Message {
			return results.Edit{}, false
		}
	}
	evaluation, _ := Evaluate(directives, nil, names)
	return edit, evaluation.Suppresses(rule, sourcetext.LineRange{First: line + shift, Last: line + shift})
}
