package registration

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/expressions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
)

// All lists every native rule in presentation order. Each rule is defined
// once, beside its checks; this list only fixes the order users see.
var All = []*define.Rule{
	correctness.VendorLockInRule,
	correctness.ContainerRunnerRule,
	suspicious.UncancellableConditionRule,
	security.MissingPermissionsRule,
	conventions.MissingActionMetadataRule,
	correctness.ExpressionSyntaxRule,
	correctness.ExpressionReferenceRule,
	correctness.ContextAvailabilityRule,
	correctness.ExpressionTypeRule,
	correctness.ExpressionCallRule,
	correctness.EventPayloadRule,
	security.ScriptInjectionRule,
	correctness.ScriptInterpolationRule,
	correctness.WorkflowRunNamesRule,
	correctness.UnsoundTernaryRule,
	correctness.SyntaxCheckRule,
	correctness.InlineSuppressionRule,
	correctness.UnusedSuppressionRule,
	correctness.MissingLocalActionRule,
	suspicious.StringConditionsRule,
	suspicious.JoinLiteralRule,
	suspicious.CollectionComparisonRule,
	suspicious.CoercionRule,
	suspicious.MixedTypeComparisonsRule,
	suspicious.CaseInsensitiveConditionsRule,
	suspicious.MixedTypeMatrixFiltersRule,
	conventions.ForbiddenSuppressionRule,
	correctness.MatrixRule,
	security.CredentialsRule,
	correctness.ShellNameRule,
	correctness.RunnerLabelRule,
	correctness.RunnerSupportRule,
	correctness.EventsRule,
	correctness.JobNeedsRule,
	correctness.ParallelStepsRule,
	correctness.ActionRule,
	correctness.RuntimeLifecycleRule,
	correctness.ExecutableBitRule,
	correctness.EnvVarRule,
	correctness.IdRule,
	correctness.GlobRule,
	correctness.PermissionsRule,
	correctness.WorkflowCallRule,
	expressions.Rule,
	correctness.DeprecatedCommandsRule,
	suspicious.ErrexitStatusRule,
	suspicious.WorkflowCommandSyntaxRule,
	correctness.InvalidOutputFileRule,
	correctness.IfCondRule,
	correctness.NumericValuesRule,
	suspicious.EmptyTriggersRule,
	suspicious.DuplicateWorkflowNameRule,
	correctness.EmptyJobRule,
	suspicious.EmptyRunRule,
	security.UnpinnedReferenceRule,
	conventions.JobTimeoutRule,
	security.CacheWriteUntrustedRule,
	security.CacheCallUnrestrictedRule,
	correctness.CacheOperationRule,
}

// Declarations returns detached native declarations in presentation order.
func Declarations() []rule.Declaration {
	out := make([]rule.Declaration, len(All))
	for i, native := range All {
		out[i] = native.ResolvedDeclaration()
	}
	return out
}

// Registrations returns check implementations in rule-declaration order.
func Registrations(rules configuration.RuleSet) []define.Registration {
	executable := make(map[string]*define.Rule)
	for _, native := range All {
		if !native.Executable() {
			continue
		}
		if _, found := rules.FindRule(native.Name); !found {
			panic("check implementation " + native.Name + " has no rule declaration")
		}
		executable[native.Name] = native
	}
	catalog := rules.Rules()
	out := make([]define.Registration, len(catalog))
	for i, metadata := range catalog {
		if native, found := executable[metadata.Name]; found {
			out[i] = native.Registration(metadata)
			continue
		}
		out[i] = define.Registration{Declaration: metadata}
	}
	return out
}

// HasRule reports whether a check implementation uses this registration name.
func HasRule(name string) bool {
	for _, native := range All {
		if native.Name == name {
			return native.Executable()
		}
	}
	return false
}
