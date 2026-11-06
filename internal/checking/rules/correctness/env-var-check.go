package correctness

import (
	envname "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/env-var"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
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
	return reporting.FromDiagnostics(envname.Check(entries))
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
