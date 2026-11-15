package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// RuntimeLifecycleRule is the single definition of the runtime-lifecycle rule.
var RuntimeLifecycleRule = &define.Rule{
	Name: "runtime-lifecycle", Description: "Report language runtimes outside their supported lifetime",
	Category: "correctness", Subcategory: "actions", Recommended: true, DefaultLevel: "warn",
	SupportsActions: true, Scope: rule.SourceConfiguration,
	Explanation: "Check Node.js release schedules for static node-version inputs to actions/setup-node. Support dates come from the Node.js Release schedule. Named lifecycle policies can notify before a known support deadline. Dynamic values, broad version ranges, release aliases, prereleases and node-version-file inputs are not resolved by this check. Node.js release support does not determine which JavaScript runtimes GitHub Actions accepts in action metadata.",
	Help:        "Select a supported Node.js release. Configure lint.lifecycle.policies to choose when support deadlines should produce notifications.",
	Checks:      func(c define.Inputs) define.Checks { return RuntimeLifecycleChecks(c.Lifecycle) },
}

func RuntimeLifecycleChecks(context actionmeta.LifecycleContext) define.Checks {
	return define.Checks{Step: func(step workflowdecl.Step) []reporting.Finding {
		action, ok := step.Action()
		if !ok {
			return nil
		}
		spec, known := workflowdecl.Scalar(action.Uses).ScalarString()
		if !known {
			return nil
		}
		target, valid := references.ParseRepositoryUses(spec)
		if !valid || !strings.EqualFold(target.Owner, "actions") || !strings.EqualFold(target.Repo, "setup-node") || target.Subpath != "" {
			return nil
		}
		version := workflowdecl.ResolvedInputs(action).Lookup("node-version")
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
		return []reporting.Finding{{
			Code: string(evidence.Kind), Severity: "warning",
			Message: message, Start: position, Step: step, DocumentationURL: evidence.Source,
			LifecycleEvidence: &evidence,
		}}
	},
	}
}
