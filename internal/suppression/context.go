// Package suppression finds workflow and composite-action locations for scoped ignores.
package suppression

import (
	"bytes"

	"go.yaml.in/yaml/v4"
)

type context struct {
	first, end                  position
	job, stepID, stepName, uses string
}

type position struct{ line, column int }

func (p position) before(other position) bool {
	return p.line < other.line || p.line == other.line && p.column < other.column
}

func start(node *yaml.Node) position { return position{node.Line, node.Column} }

// Index records concrete YAML job and step ranges, without following aliases.
type Index []context

// Contexts indexes the first YAML document. Invalid YAML has no structural scopes.
func Contexts(source []byte, resolve func(string) *string) Index {
	var document yaml.Node
	if yaml.Unmarshal(source, &document) != nil || len(document.Content) == 0 {
		return nil
	}
	var result Index
	scalar := func(node *yaml.Node, key string) string {
		value := rawScalar(node, key)
		if resolve != nil {
			if resolved := resolve(value); resolved != nil {
				return *resolved
			}
		}
		return value
	}
	var visit func(*yaml.Node, position, []string, context)
	var steps func(*yaml.Node, position, string)
	steps = func(node *yaml.Node, end position, job string) {
		if node.Kind != yaml.SequenceNode {
			return
		}
		for i, step := range node.Content {
			last := end
			if i+1 < len(node.Content) {
				last = start(node.Content[i+1])
			}
			if step.Kind != yaml.MappingNode {
				continue
			}
			result = append(result, context{first: start(step), end: last, job: job, stepID: scalar(step, "id"), stepName: scalar(step, "name"), uses: scalar(step, "uses")})
			for j := 0; j+1 < len(step.Content); j += 2 {
				if step.Content[j].Value != "parallel" {
					continue
				}
				parallelEnd := last
				if j+2 < len(step.Content) {
					parallelEnd = start(step.Content[j+2])
				}
				steps(step.Content[j+1], parallelEnd, job)
			}
		}
	}
	visit = func(node *yaml.Node, end position, path []string, current context) {
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				last := end
				if i+2 < len(node.Content) {
					last = start(node.Content[i+2])
				}
				next := current
				if len(path) == 1 && path[0] == "jobs" {
					next = context{first: start(key), end: last, job: key.Value, uses: scalar(value, "uses")}
					result = append(result, next)
				}
				visit(value, last, append(append([]string(nil), path...), key.Value), next)
			}
		} else if node.Kind == yaml.SequenceNode && (len(path) == 3 && path[0] == "jobs" && path[2] == "steps" || len(path) == 2 && path[0] == "runs" && path[1] == "steps") {
			steps(node, end, current.job)
		}
	}
	visit(document.Content[0], position{bytes.Count(source, []byte("\n")) + 2, 1}, nil, context{})
	return result
}

func rawScalar(node *yaml.Node, key string) string {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key && node.Content[i+1].Kind == yaml.ScalarNode {
				return node.Content[i+1].Value
			}
		}
	}
	return ""
}

// Match requires all structural selectors to identify the same job or step.
func (index Index) Match(line, column int, job, step, uses string) bool {
	if job == "" && step == "" && uses == "" {
		return true
	}
	pos := position{line, column}
	for _, location := range index {
		contains := !pos.before(location.first) && pos.before(location.end)
		if column == 0 {
			contains = location.first.line <= line && (line < location.end.line || line == location.end.line && location.end.column > 1)
			// A sourced finding records the invoking line but no column. Do not
			// choose between distinct flow-style constructs on that same line.
			for _, other := range index {
				if other.first.line > line || other.end.line < line {
					continue
				}
				if !other.first.before(location.end) || !location.first.before(other.end) {
					contains = false
					break
				}
			}
		}
		if contains && (job == "" || job == location.job) && (step == "" || step == location.stepID || step == location.stepName) && (uses == "" || uses == location.uses) {
			return true
		}
	}
	return false
}
