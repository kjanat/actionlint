// Package editor supplies workflow language features without an LSP transport.
package editor

import (
	"strings"
	"unicode/utf8"

	expr "actionlint.kjanat.dev/internal/expression"
	workflow "actionlint.kjanat.dev/internal/workflow"
	workflowcontext "actionlint.kjanat.dev/internal/workflow/context"
	"actionlint.kjanat.dev/internal/workflow/eventpayload"
	"actionlint.kjanat.dev/internal/yaml"
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
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil
	}
	key := keyAt(&root, content, offset)
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
	w, _ := workflow.Parse([]byte(content))
	c.UpdateEvent(eventpayload.Type(w))
	if w != nil {
		for _, event := range w.On {
			if dispatch, ok := event.(*workflow.WorkflowDispatchEvent); ok {
				inputs := expr.NewEmptyStrictObjectType()
				for name, input := range dispatch.Inputs {
					var ty expr.Type = expr.StringType{}
					switch input.Type {
					case workflow.WorkflowDispatchEventInputTypeBoolean:
						ty = expr.BoolType{}
					case workflow.WorkflowDispatchEventInputTypeNumber:
						ty = expr.NumberType{}
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
func keyAt(root *yaml.Node, content string, offset int) string {
	before := content[:offset]
	line := strings.Count(before, "\n") + 1
	column := utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:]) + 1
	bestLine, bestColumn := 0, 0
	key := ""
	var walk func(*yaml.Node, []string)
	walk = func(n *yaml.Node, path []string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, child := range n.Content {
				walk(child, path)
			}
		case yaml.MappingNode:
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
		case yaml.ScalarNode:
			if (n.Line < line || n.Line == line && n.Column <= column) && (n.Line > bestLine || n.Line == bestLine && n.Column > bestColumn) {
				bestLine, bestColumn = n.Line, n.Column
				key = strings.Join(path, ".")
			}
		}
	}
	walk(root, nil)
	for key != "" {
		if contexts, _ := workflowcontext.WorkflowKeyAvailability(key); len(contexts) > 0 {
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
