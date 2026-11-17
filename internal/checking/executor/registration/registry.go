package registration

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/expressions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"
	"actionlint.kjanat.dev/v2/internal/model/rule"
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

// Find returns the native rule named name.
func Find(name string) (*define.Rule, bool) {
	for _, native := range All {
		if native.Name == name {
			return native, true
		}
	}
	return nil, false
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
