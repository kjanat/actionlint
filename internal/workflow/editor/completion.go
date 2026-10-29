// Package editor supplies workflow language features without an LSP transport.
package editor

import (
	"strings"
	"unicode/utf8"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	expr "actionlint.kjanat.dev/v2/internal/expression"
	contextspec "actionlint.kjanat.dev/v2/internal/expression/context"
	workflow "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/eventpayload"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// Complete returns expression property edits against the supplied buffer.
func Complete(content string, offset int) []expr.Completion {
	if offset < 0 || offset > len(content) {
		return nil
	}
	before := content[:offset]
	lineStart := strings.LastIndexByte(before, '\n') + 1
	if strings.HasPrefix(strings.TrimSpace(before[lineStart:]), "#") {
		return nil
	}
	start := strings.LastIndex(before, "${{")
	root, err := codec.Parse([]byte(content))
	if err != nil {
		return nil
	}
	key := keyAt(root, content, offset)
	if start >= 0 && !strings.Contains(before[start+3:], "}}") {
		start += 3
	} else {
		// Conditions also accept bare expressions.
		if !strings.HasSuffix(key, ".if") {
			return nil
		}
		colon := strings.IndexByte(content[lineStart:], ':')
		if colon < 0 || lineStart+colon >= offset {
			return nil
		}
		start = lineStart + colon + 1
	}
	c := expr.NewChecker(false, nil)
	c.SetWorkflowKeyAvailability(key)
	parsed, _ := workflowparser.Parse([]byte(content))
	w := declaration.Publish(content, parsed, false, "").Bind("editor-buffer").Workflow()
	c.UpdateEvent(eventpayload.Type(w))
	if w.Present() {
		for event := range w.On() {
			if dispatch, ok := event.WorkflowDispatchEvent(); ok {
				inputs := expr.NewEmptyStrictObjectType()
				for name, input := range dispatch.Inputs() {
					var ty expr.Type = expr.StringType{}
					switch input.Type() {
					case workflow.WorkflowDispatchEventInputTypeBoolean:
						ty = expr.BoolType{}
					case workflow.WorkflowDispatchEventInputTypeNumber:
						ty = expr.NumberType{}
					case workflow.WorkflowDispatchEventInputTypeNone,
						workflow.WorkflowDispatchEventInputTypeString,
						workflow.WorkflowDispatchEventInputTypeChoice,
						workflow.WorkflowDispatchEventInputTypeEnvironment:
					}
					inputs.Props[strings.ToLower(name)] = ty
				}
				c.UpdateDispatchInputs(inputs)
			}
		}
	}
	result := c.Complete(content[start:], offset-start)
	for i := range result {
		result[i].Start += start
		result[i].End += start
	}
	return result
}

// keyAt follows the owned YAML tree to the scalar preceding the cursor. Block
// scalars keep their mapping address across lines; sequence indices do not enter
// the context-availability vocabulary.
func keyAt(root *syntax.Node, content string, offset int) string {
	before := content[:offset]
	line := strings.Count(before, "\n") + 1
	column := utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:]) + 1
	bestLine, bestColumn := 0, 0
	key := ""
	var walk func(*syntax.Node, []string)
	walk = func(n *syntax.Node, path []string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case syntax.DocumentNode, syntax.SequenceNode:
			for _, child := range n.Content {
				walk(child, path)
			}
		case syntax.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				name := n.Content[i].Value
				if len(path) == 1 && path[0] == "jobs" {
					name = "<job_id>"
				}
				if len(path) > 0 && (path[len(path)-1] == "services") {
					name = "<service_id>"
				}
				next := append(append([]string{}, path...), name)
				walk(n.Content[i+1], next)
			}
		case syntax.ScalarNode:
			if (n.Line < line || n.Line == line && n.Column <= column) && (n.Line > bestLine || n.Line == bestLine && n.Column > bestColumn) {
				bestLine, bestColumn = n.Line, n.Column
				key = strings.Join(path, ".")
			}
		case syntax.AliasNode:
		}
	}
	walk(root, nil)
	for key != "" {
		if contexts, _ := contextspec.WorkflowKeyAvailability(key); len(contexts) > 0 {
			return key
		}
		at := strings.LastIndexByte(key, '.')
		if at < 0 {
			break
		}
		key = key[:at]
	}
	return ""
}
