package checking

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/bundled"
)

var builtins = []rule.Declaration{
	correctness.VendorLockInDeclaration(),
	correctness.ContainerRunnerDeclaration(),
	suspicious.UncancellableConditionCheck().Declaration,
	security.MissingPermissionsDeclaration(),
	conventions.MissingActionMetadataDeclaration(),
	correctness.ExpressionSyntaxDeclaration(),
	correctness.ExpressionReferenceDeclaration(),
	correctness.ContextAvailabilityDeclaration(),
	correctness.ExpressionTypeDeclaration(),
	correctness.ExpressionCallDeclaration(),
	correctness.EventPayloadDeclaration(),
	security.ScriptInjectionDeclaration(),
	correctness.ScriptInterpolationDeclaration(),
	correctness.WorkflowRunNamesDeclaration(),
	correctness.UnsoundTernaryDeclaration(),
	correctness.SyntaxCheckDeclaration(),
	correctness.InlineSuppressionDeclaration(),
	correctness.UnusedSuppressionDeclaration(),
	correctness.MissingLocalActionDeclaration(),
	suspicious.StringConditionsDeclaration(),
	suspicious.JoinLiteralDeclaration(),
	suspicious.CollectionComparisonDeclaration(),
	suspicious.CoercionDeclaration(),
	suspicious.MixedTypeComparisonsDeclaration(),
	suspicious.CaseInsensitiveConditionsDeclaration(),
	suspicious.MixedTypeMatrixFiltersDeclaration(),
	conventions.ForbiddenSuppressionDeclaration(),
	correctness.MatrixDeclaration(),
	security.CredentialsCheck().Declaration,
	correctness.ShellNameDeclaration(),
	correctness.RunnerLabelDeclaration(),
	correctness.EventsCheck().Declaration,
	correctness.JobNeedsCheck().Declaration,
	correctness.ParallelStepsCheck().Declaration,
	correctness.ActionDeclaration(),
	correctness.ExecutableBitDeclaration(),
	correctness.EnvVarCheck().Declaration,
	correctness.IdCheck().Declaration,
	correctness.GlobCheck().Declaration,
	correctness.PermissionsCheck().Declaration,
	correctness.WorkflowCallDeclaration(),
	correctness.ExpressionDeclaration(),
	correctness.DeprecatedCommandsDeclaration(),
	suspicious.ErrexitStatusDeclaration(),
	suspicious.WorkflowCommandSyntaxDeclaration(),
	correctness.InvalidOutputFileDeclaration(),
	correctness.IfCondCheck().Declaration,
	correctness.NumericValuesCheck().Declaration,
	suspicious.EmptyTriggersCheck().Declaration,
	correctness.EmptyJobCheck().Declaration,
	suspicious.EmptyRunCheck().Declaration,
	security.UnpinnedReferenceDeclaration(),
	conventions.JobTimeoutDeclaration(),
	security.CacheWriteUntrustedDeclaration(),
	security.CacheCallUnrestrictedDeclaration(),
	correctness.CacheOperationDeclaration(),
}

// providerRules reads rule descriptors from the bundled provider manifests.
// Invalid embedded metadata causes a panic. This package reads only the rule
// field to avoid importing the provider model and its analyzer dependency.
// TestProviderRulesComeFromManifests compares the result with provider.Bundled.
var providerRules = sync.OnceValue(func() []rule.Descriptor {
	names := bundled.Names()
	rules := make([]rule.Descriptor, 0, len(names))
	for _, name := range names {
		data, _ := bundled.Manifest(name)
		var manifest struct {
			Rule rule.Descriptor `json:"rule"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.Rule.Name != name {
			panic(fmt.Sprintf("bundled tool provider %s: invalid rule: %v", name, err))
		}
		rules = append(rules, manifest.Rule)
	}
	return rules
})

// BuiltinRuleDeclarations returns the built-in rules and their configuration requirements.
// Each descriptor's slices belong to the caller.
func BuiltinRuleDeclarations() []rule.Declaration {
	declarations := slices.Clone(builtins)
	for i := range declarations {
		declarations[i].Descriptor = declarations[i].Clone()
	}
	return declarations
}

// ListRules lists the built-in checks followed by the bundled tool providers' rules.
func ListRules() []rule.Descriptor {
	rules := make([]rule.Descriptor, 0, len(builtins)+len(providerRules()))
	for _, declaration := range builtins {
		rules = append(rules, declaration.Descriptor)
	}
	rules = append(rules, providerRules()...)
	for i := range rules {
		rules[i] = rules[i].Clone()
	}
	return rules
}

// FindRule returns the built-in or bundled provider rule named name.
func FindRule(name string) (rule.Descriptor, bool) {
	name = ResolveRuleAlias(name)
	for _, r := range builtins {
		if r.Name == name {
			return r.Clone(), true
		}
	}
	for _, r := range providerRules() {
		if r.Name == name {
			return r.Clone(), true
		}
	}
	return rule.Descriptor{}, false
}

// IsProviderRule reports whether name is the rule of a bundled tool provider.
func IsProviderRule(name string) bool {
	return slices.ContainsFunc(providerRules(), func(r rule.Descriptor) bool { return r.Name == name })
}

// RuleCategory returns the semantic category used by configuration and reference
// material, independently of provider identity or experimental maturity.
func RuleCategory(name string) string {
	descriptor, ok := FindRule(name)
	if !ok {
		return ""
	}
	return descriptor.Category
}
