package correctness

import (
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/runtimes"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func RuntimeLifecycleDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "runtime-lifecycle", Description: "Report language runtimes outside their supported lifetime",
		Category: "correctness", Subcategory: "actions", Maturity: "stable", Provider: "actionlint",
		Recommended: true, DefaultLevel: "warn",
		SupportsActions: true, Scope: rule.SourceConfiguration,
		Explanation:      "Check Node.js release schedules for static node-version inputs to actions/setup-node. Support dates come from the Node.js Release schedule. Named lifecycle policies can notify before a known support deadline. Dynamic values, broad version ranges, release aliases, prereleases and node-version-file inputs are not resolved by this check. Node.js release support does not determine which JavaScript runtimes GitHub Actions accepts in action metadata.",
		Help:             "Select a supported Node.js release. Configure lint.lifecycle.policies to choose when support deadlines should produce notifications.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#runtime-lifecycle",
	}
}

func RuntimeLifecycleCheck(context actionmeta.LifecycleContext) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: RuntimeLifecycleDeclaration(),
		Step: func(step workflowdecl.StepView) []reporting.Finding {
			action, ok := step.Action()
			if !ok {
				return nil
			}
			spec, known := action.Uses().Semantic().ScalarString()
			if !known {
				return nil
			}
			target, valid := references.ParseRepositoryUses(spec)
			if !valid || !strings.EqualFold(target.Owner, "actions") || !strings.EqualFold(target.Repo, "setup-node") || target.Subpath != "" {
				return nil
			}
			version := action.ResolvedInputs().Lookup("node-version")
			selector, known := version.ScalarString()
			if !known {
				return nil
			}
			evidence, message, known := runtimes.NodeLifecycle(strings.TrimSpace(selector), context.AsOf, context.Service)
			if !known {
				return nil
			}
			position := version.Origin().Span.Start
			if position.Line == 0 {
				return nil
			}
			return []reporting.Finding{{
				Rule: "runtime-lifecycle", Code: string(evidence.Kind), Severity: "warning",
				Message: message, Start: position, Step: step, DocumentationURL: evidence.Source,
				LifecycleEvidence: &evidence,
			}}
		},
	}
}
