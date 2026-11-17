package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results"
)

// VendorLockInChecks binds the execution target to job and step reference checks.
func VendorLockInChecks(target string) define.Checks {
	return define.Checks{
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			if !job.Present() || job.WorkflowCall == nil {
				return nil
			}
			return reporting.FromDiagnostics(VendorLockIn(job.Workflow(), job.WorkflowCall.Uses, target))
		},
		Step: func(step workflows.BoundStep) []reporting.StepFinding {
			if action, present := step.Action(); present {
				findings := reporting.FromDiagnostics(VendorLockIn(step.Workflow(), action.Uses, target))
				for i := range findings {
					findings[i].Step = step
				}
				return findings
			}
			return nil
		},
	}
}

// VendorLockInRule is the single definition of the vendor-lock-in rule.
var VendorLockInRule = &define.Rule{
	Name:        "vendor-lock-in",
	Description: "Report repository URL references unsupported by GitHub",
	Category:    "correctness",
	Subcategory: "dependencies",
	Recommended: true,
	Explanation: "GitHub Actions accepts repository shorthand in uses. It does not accept full repository URLs. On the github execution target, report URL references in action steps and reusable-workflow calls. An HTTPS github.com URL offers a safe fix that removes its prefix while preserving the repository, action path and ref. Other hosts have no automatic substitution: the same owner/repository name on GitHub does not establish the same action. Docker registry URLs, local actions and dynamic references are excluded. Other execution targets are not restricted by this rule.",
	Help:        "Use owner/repo[/path]@ref for an action hosted in the current GitHub instance. To use an externally hosted action, choose and verify a GitHub mirror, or check out the external source and invoke its local action. The prefix fix is unavailable for URLs with credentials, ports, URL encoding, queries, fragments or invalid shorthand.",
	Examples: []rule.Example{{
		Description: "Remove the GitHub URL prefix without changing the action or its version.",
		Before:      "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: https://github.com/mlugg/setup-zig@v2\n",
		After:       "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: mlugg/setup-zig@v2\n",
	}},
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	Checks:          func(c define.RuleInputs) define.Checks { return VendorLockInChecks(c.Runners.Target()) },
}

func VendorLockIn(w workflows.BoundWorkflow, uses *definition.String, target string) []results.Diagnostic {
	if target != "github" || uses == nil || uses.ContainsExpression() {
		return nil
	}
	ref, present := callreferences.ParseRepositoryURL(uses.Value)
	if !present {
		return nil
	}
	location, _ := uses.Position()
	finding := results.Diagnostic{
		Severity: severity.Error, Start: location,
		Message: fmt.Sprintf("GitHub Actions requires owner/repo[/path]@ref in uses, not a full repository URL (host %q)", ref.Host()),
	}
	if ref.Shorthand() != "" {
		finding.Message = fmt.Sprintf("GitHub Actions requires repository shorthand in uses; write %q instead of the full GitHub URL", ref.Shorthand())
		if source := w.ScalarSource(uses); source != nil {
			start, startOK := source.Position(1, 1)
			end, endOK := source.EndPos(1, ref.PrefixLength()+1)
			if startOK && endOK {
				finding.Fixes = []results.Fix{{
					Description: "Remove the GitHub URL prefix", Applicability: applicability.Safe,
					Edits: []results.Edit{{Start: *start, End: *end}},
				}}
			}
		}
	}
	return []results.Diagnostic{finding}
}
