package correctness

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
	"actionlint.kjanat.dev/v2/internal/results"
)

// EnvVarRule is the single definition of the env-var rule.
var EnvVarRule = &define.Rule{
	Name:        "env-var",
	Description: "Validate environment variable declarations",
	Category:    "correctness",
	Subcategory: "environment",
	Recommended: true,
	Help:        "Use valid environment variable names and values in the indicated mapping.",
	Examples: []rule.Example{rule.WorkflowExample("Use environment names without spaces.",
		"    env:\n      BAD NAME: value\n", "    env:\n      GOOD_NAME: value\n")},
	Checks: define.Fixed(define.Checks{Workflow: checkWorkflowEnvironment, Job: checkJobEnvironment, Step: checkStepEnvironment}),
}

func checkEnvironmentMapping(env *workflowlang.Env) []reporting.StepFinding {
	if env == nil {
		return nil
	}
	var entries []workflowfacts.Entry
	resolved := declaration.Environment(env)
	for key := range resolved.KeyedEntries() {
		if name, known := key.ScalarString(); known {
			entries = append(entries, workflowfacts.Entry{Name: name, State: mapping.KnownKey, Start: key.Origin().Span.Start})
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
		if entry.State == mapping.UnknownKey || !strings.ContainsAny(entry.Name, "&= 	") {
			continue
		}
		findings = append(findings, results.Diagnostic{Message: fmt.Sprintf("environment variable name %q is invalid. '&', '=' and spaces should not be contained", entry.Name), Start: entry.Start})
	}
	return findings
}
