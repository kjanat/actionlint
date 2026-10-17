// Package identifier validates complete identifier scopes without visitor state.
package identifier

import (
	"fmt"
	"regexp"
	"strings"

	"actionlint.kjanat.dev/diagnostic"
)

var pattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

func Valid(value string) bool { return pattern.MatchString(value) }

type Name struct {
	Value   string
	Dynamic bool
	Start   diagnostic.Position
}
type Job struct {
	ID           *Name
	Needs, Steps []Name
}

func Check(job Job) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	report := func(name Name, message string) {
		findings = append(findings, diagnostic.Diagnostic{Rule: "id", Start: name.Start, Message: message})
	}
	convention := func(name Name, kind string) {
		if name.Value != "" && !name.Dynamic && !Valid(name.Value) {
			report(name, fmt.Sprintf("invalid %s ID %q. %s ID must start with a letter or _ and contain only alphanumeric characters, -, or _", kind, name.Value, kind))
		}
	}
	if job.ID != nil {
		convention(*job.ID, "job")
	}
	for _, name := range job.Needs {
		convention(name, "job")
	}
	seen := map[string]diagnostic.Position{}
	for _, name := range job.Steps {
		convention(name, "step")
		if strings.HasPrefix(name.Value, "__") {
			report(name, fmt.Sprintf("step ID %q must not start with reserved prefix __", name.Value))
		}
		if len(name.Value) >= 100 {
			report(name, fmt.Sprintf("step ID %q must be shorter than 100 characters", name.Value))
		}
		id := strings.ToLower(name.Value)
		if prev, ok := seen[id]; ok {
			report(name, fmt.Sprintf("step ID %q duplicates. previously defined at line:%d,col:%d. step ID must be unique within a job. note that step ID is case insensitive", name.Value, prev.Line, prev.Column))
		} else {
			seen[id] = name.Start
		}
	}
	return findings
}
