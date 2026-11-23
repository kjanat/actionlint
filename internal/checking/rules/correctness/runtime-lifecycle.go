package correctness

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var RuntimeLifecycleRule = &define.Rule{
	Name:            "runtime-lifecycle",
	Description:     "Report language runtimes outside their supported lifetime",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	DefaultLevel:    "warn",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	Explanation:     "Check static node-version inputs to actions/setup-node against the Node.js release schedule.",
	Help:            "Select a supported Node.js release. Set lint.lifecycle.policies to choose when deadlines produce notifications.",
	Checks:          func(c define.RuleInputs) define.Checks { return RuntimeLifecycleChecks(c.Lifecycle) },
}

func RuntimeLifecycleChecks(context actionmeta.LifecycleContext) define.Checks {
	return define.Checks{
		Step: func(step workflows.BoundStep) []reporting.StepFinding {
			action, ok := step.Action()
			if !ok {
				return nil
			}
			spec, known := workflows.Scalar(action.Uses).ScalarString()
			if !known {
				return nil
			}
			target, valid := callreferences.ParseRepositoryUses(spec)
			if !valid || !strings.EqualFold(target.Owner, "actions") || !strings.EqualFold(target.Repo, "setup-node") || target.Subpath != "" {
				return nil
			}
			version := workflows.ResolvedInputs(action).Lookup("node-version")
			selector, known := version.ScalarString()
			if !known {
				return nil
			}
			evidence, message, known := lifecycle.NodeLifecycle(strings.TrimSpace(selector), context.AsOf, context.Service)
			if !known {
				return nil
			}
			position := version.Origin().Span.Start
			if position.Line == 0 {
				return nil
			}
			return []reporting.StepFinding{{
				Code: string(evidence.Kind), Severity: severity.Warning,
				Message: message, Start: position, Step: step, DocumentationURL: evidence.Source,
				LifecycleEvidence: &evidence,
			}}
		},
	}
}
