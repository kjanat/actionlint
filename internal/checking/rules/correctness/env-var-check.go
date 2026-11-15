package correctness

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

func checkEnvironmentMapping(env *workflowlang.Env) []reporting.StepFinding {
	if env == nil {
		return nil
	}
	var entries []workflowfacts.Entry
	resolved := declaration.Environment(env)
	for key := range resolved.KeyedEntries() {
		if name, known := key.ScalarString(); known {
			entries = append(entries, workflowfacts.Entry{Name: name, State: workflowfacts.KnownKey, Start: key.Origin().Span.Start})
		}
	}
	return reporting.FromDiagnostics(environmentNameDiagnostics(entries))
}

func checkWorkflowEnvironment(w declaration.Workflow) []reporting.StepFinding {
	return checkEnvironmentMapping(w.Env)
}

func checkStepEnvironment(step declaration.Step) []reporting.StepFinding {
	return checkEnvironmentMapping(step.Env)
}

func checkJobEnvironment(job declaration.Job) []reporting.StepFinding {
	out := checkEnvironmentMapping(job.Env)
	if job.Container != nil {
		out = append(out, checkEnvironmentMapping(job.Container.Env)...)
	}
	if job.Services != nil {
		for _, service := range job.Services.Value {
			if service != nil && service.Container != nil {
				out = append(out, checkEnvironmentMapping(service.Container.Env)...)
			}
		}
	}
	return out
}

// environmentNameDiagnostics reports resolved environment names that contain '&', '=' or whitespace.
func environmentNameDiagnostics(entries []workflowfacts.Entry) []results.Diagnostic {
	var findings []results.Diagnostic
	for _, entry := range entries {
		if entry.State == workflowfacts.UnknownKey || !strings.ContainsAny(entry.Name, "&= 	") {
			continue
		}
		findings = append(findings, results.Diagnostic{Rule: "env-var", Message: fmt.Sprintf("environment variable name %q is invalid. '&', '=' and spaces should not be contained", entry.Name), Start: entry.Start})
	}
	return findings
}
