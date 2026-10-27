// Package suppression finds workflow and composite-action locations for scoped ignores.
package suppression

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

type context struct {
	first, end                  position
	job, stepID, stepName, uses string
	startsLine                  bool
}

type position struct{ line, column int }

func (p position) before(other position) bool {
	return p.line < other.line || p.line == other.line && p.column < other.column
}

func start(node *syntax.Node) position { return position{node.Line, node.Column} }

// Index records concrete YAML job and step ranges, without following aliases.
type Index []context

// Contexts indexes the first YAML document. Invalid YAML has no structural scopes.
func Contexts(source []byte, resolve func(string) *string) Index {
	document, err := codec.Parse(source)
	if err != nil || len(document.Content) == 0 {
		return nil
	}
	var result Index
	lines := sourcemap.NewDocument(string(source)).Lines
	startsLine := func(node *syntax.Node) bool {
		if node.Line < 1 || node.Line > len(lines) {
			return false
		}
		prefix := []rune(lines[node.Line-1])
		if node.Column < 1 || node.Column > len(prefix)+1 {
			return false
		}
		return strings.Trim(string(prefix[:node.Column-1]), " \t-") == ""
	}
	scalar := func(node *syntax.Node, key string) string {
		value := rawScalar(node, key)
		if resolve != nil {
			if resolved := resolve(value); resolved != nil {
				return *resolved
			}
		}
		return value
	}
	var visit func(*syntax.Node, position, []string, context)
	var steps func(*syntax.Node, position, string)
	steps = func(node *syntax.Node, end position, job string) {
		if node.Kind != syntax.SequenceNode {
			return
		}
		for i, step := range node.Content {
			last := end
			if i+1 < len(node.Content) {
				last = start(node.Content[i+1])
			}
			if step.Kind != syntax.MappingNode {
				continue
			}
			result = append(result, context{first: start(step), end: last, job: job, stepID: scalar(step, "id"), stepName: scalar(step, "name"), uses: scalar(step, "uses"), startsLine: startsLine(step)})
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
	visit = func(node *syntax.Node, end position, path []string, current context) {
		if node.Kind == syntax.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				last := end
				if i+2 < len(node.Content) {
					last = start(node.Content[i+2])
				}
				next := current
				if len(path) == 1 && path[0] == "jobs" {
					next = context{first: start(key), end: last, job: key.Value, uses: scalar(value, "uses"), startsLine: startsLine(key)}
					result = append(result, next)
				}
				visit(value, last, append(append([]string(nil), path...), key.Value), next)
			}
		} else if node.Kind == syntax.SequenceNode && (len(path) == 3 && path[0] == "jobs" && path[2] == "steps" || len(path) == 2 && path[0] == "runs" && path[1] == "steps") {
			steps(node, end, current.job)
		}
	}
	visit(document.Content[0], position{len(lines) + 1, 1}, nil, context{})
	return result
}

func rawScalar(node *syntax.Node, key string) string {
	if node.Kind == syntax.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key && node.Content[i+1].Kind == syntax.ScalarNode {
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
				if other.first.line > line || other.end.line < line || other.end.line == line && location.first.line == line && location.startsLine {
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
