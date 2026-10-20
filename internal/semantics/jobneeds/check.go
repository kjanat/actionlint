// Package jobneeds checks a complete dependency graph, independently of traversal.
package jobneeds

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
)

type Name struct {
	Value string
	Start diagnostic.Position
}
type Job struct {
	ID    Name
	Needs []Name
}
type node struct {
	id    string
	needs []string
	start diagnostic.Position
}

// Check resolves all declarations before finding cycles. The supplied jobs are
// source ordered; graph walks use sorted IDs for deterministic cycle selection.
func Check(jobs []Job) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	report := func(pos diagnostic.Position, message string) {
		findings = append(findings, diagnostic.Diagnostic{Rule: "job-needs", Start: pos, Message: message})
	}
	nodes := map[string]node{}
	for _, job := range jobs {
		var needs []string
		for _, dep := range job.Needs {
			id := strings.ToLower(dep.Value)
			if slices.Contains(needs, id) {
				report(dep.Start, fmt.Sprintf("job ID %q duplicates in \"needs\" section. note that job ID is case insensitive", dep.Value))
				continue
			}
			if id != "" {
				needs = append(needs, id)
			}
		}
		id := strings.ToLower(job.ID.Value)
		if id == "" {
			continue
		}
		if previous, ok := nodes[id]; ok {
			report(job.ID.Start, fmt.Sprintf("job ID %q duplicates. previously defined at line:%d,col:%d. note that job ID is case insensitive", job.ID.Value, previous.start.Line, previous.start.Column))
		}
		nodes[id] = node{id, needs, job.ID.Start}
	}
	keys := slices.Sorted(maps.Keys(nodes))
	valid := true
	for _, id := range keys {
		for _, dep := range nodes[id].needs {
			if _, exists := nodes[dep]; !exists {
				report(nodes[id].start, fmt.Sprintf("job %q needs job %q which does not exist in this workflow", id, dep))
				valid = false
			}
		}
	}
	if !valid {
		return findings
	}
	state := map[string]uint8{}
	var stack, cycle []string
	var visit func(string) bool
	visit = func(id string) bool {
		state[id] = 1
		stack = append(stack, id)
		for _, dep := range nodes[id].needs {
			if state[dep] == 1 {
				cycle = slices.Clone(stack[slices.Index(stack, dep):])
				return true
			}
			if state[dep] == 0 && visit(dep) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return false
	}
	for _, id := range keys {
		if state[id] == 0 && visit(id) {
			break
		}
	}
	if len(cycle) > 0 {
		first := 0
		for i, id := range cycle {
			pos, start := nodes[id].start, nodes[cycle[first]].start
			if pos.Line < start.Line || pos.Line == start.Line && pos.Column < start.Column {
				first = i
			}
		}
		ordered := append(slices.Clone(cycle[first:]), cycle[:first]...)
		ordered = append(ordered, ordered[0])
		quoted := make([]string, len(ordered))
		for i, id := range ordered {
			quoted[i] = strconv.Quote(id)
		}
		report(nodes[ordered[0]].start, "cyclic dependencies in \"needs\" job configurations are detected. detected cycle is "+strings.Join(quoted, " -> "))
	}
	return findings
}
