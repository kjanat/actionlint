package correctness

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
	"actionlint.kjanat.dev/v2/internal/results"
)

var EnvVarRule = &define.Rule{
	Name:        "env-var",
	Description: "Validate environment variable declarations",
	Category:    "correctness",
	Subcategory: "environment",
	Recommended: true,
	Help:        "Use valid variable names and values.",
	Examples: []rule.Example{rule.WorkflowExample("Remove spaces from the name.",
		"    env:\n      BAD NAME: value\n", "    env:\n      GOOD_NAME: value\n")},
	Checks: define.Fixed(define.Checks{Workflow: checkWorkflowEnvironment, Job: checkJobEnvironment, Step: checkStepEnvironment}),
}

func checkEnvironmentMapping(env *definition.Env) []reporting.StepFinding {
	if env == nil {
		return nil
	}
	return reporting.FromDiagnostics(environmentNameDiagnostics(slices.Collect(workflows.Environment(env).Declarations())))
}

func checkWorkflowEnvironment(w workflows.BoundWorkflow) []reporting.StepFinding {
	return checkEnvironmentMapping(w.Env)
}

func checkStepEnvironment(step workflows.BoundStep) []reporting.StepFinding {
	return checkEnvironmentMapping(step.Env)
}

func checkJobEnvironment(job workflows.BoundJob) []reporting.StepFinding {
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

func environmentNameDiagnostics(entries []mappings.Declaration) []results.Diagnostic {
	var findings []results.Diagnostic
	for _, entry := range entries {
		if entry.State == mappings.UnknownKey || !strings.ContainsAny(entry.Name, "&= 	") {
			continue
		}
		findings = append(findings, results.Diagnostic{Message: fmt.Sprintf("environment variable name %q is invalid. '&', '=' and spaces should not be contained", entry.Name), Start: entry.Origin.Span.Start})
	}
	return findings
}
