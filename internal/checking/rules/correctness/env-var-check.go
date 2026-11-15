package correctness

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

func checkEnvironmentMapping(env declaration.EnvView) []reporting.Finding {
	if !env.Present() {
		return nil
	}
	var entries []workflowfacts.Entry
	resolved := env.Mapping()
	for key := range resolved.KeyedEntries() {
		if name, known := key.ScalarString(); known {
			entries = append(entries, workflowfacts.Entry{Name: name, State: workflowfacts.KnownKey, Start: key.Origin().Span.Start})
		}
	}
	return reporting.FromDiagnostics(environmentNameDiagnostics(entries))
}

func checkWorkflowEnvironment(w declaration.WorkflowView) []reporting.Finding {
	return checkEnvironmentMapping(w.Env())
}

func checkStepEnvironment(step declaration.StepView) []reporting.Finding {
	return checkEnvironmentMapping(step.Env())
}

func checkJobEnvironment(job declaration.JobView) []reporting.Finding {
	out := checkEnvironmentMapping(job.Env())
	if job.Container().Present() {
		out = append(out, checkEnvironmentMapping(job.Container().Env())...)
	}
	if job.Services().Present() {
		for _, service := range job.Services().Value() {
			out = append(out, checkEnvironmentMapping(service.Container().Env())...)
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
