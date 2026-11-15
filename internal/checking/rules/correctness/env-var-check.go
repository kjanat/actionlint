package correctness

import (
	envname "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/env-var"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func checkEnvironmentMapping(env *workflowlang.Env) []reporting.Finding {
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
	return reporting.FromDiagnostics(envname.Check(entries))
}

func checkWorkflowEnvironment(w declaration.Workflow) []reporting.Finding {
	return checkEnvironmentMapping(w.Env)
}

func checkStepEnvironment(step declaration.Step) []reporting.Finding {
	return checkEnvironmentMapping(step.Env)
}

func checkJobEnvironment(job declaration.Job) []reporting.Finding {
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
