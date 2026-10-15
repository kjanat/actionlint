package engine

import (
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/ruff"
	"actionlint.kjanat.dev/internal/workflownames"
)

// ruleDescriptor supplies metadata to both rule instances and frontend discovery.
type ruleConfigurationScope uint8

const (
	callerConfiguration ruleConfigurationScope = iota
	sourceConfiguration
	compositeConfiguration
)

type ruleDescriptor struct {
	requiresScriptExecution bool
	buildScripts            func(ruleContext) (scriptAnalyzer, error)
	configurationScope      ruleConfigurationScope
	skipWhenDisabled        bool
	required                func(ruleContext) bool
	supportsActions         bool
	Name                    string `json:"name"`
	Description             string `json:"description"`
	Category                string `json:"category"`
	Recommended             bool   `json:"recommended"`
	newOptions              func() any
	configure               func(*Config, RuleSetting)
	build                   func(ruleContext) (Rule, error)
	enabled                 func(ruleContext) bool
}
type ruleContext struct {
	workflowNames           *workflownames.Index
	ruff                    string
	ruffOptions             *ExternalCommandOptions
	ruffCompatibility       *ruff.Compatibility
	ruffWarning             func(error)
	path                    string
	config                  *Config
	projectConfig           *Config
	actions                 *LocalActionsCache
	workflows               *LocalReusableWorkflowCache
	process                 *concurrentProcess
	shellcheck              string
	shellcheckRunner        func([]string, string) ([]byte, error)
	debug                   func(string, ...any)
	shellcheckOptions       *ExternalCommandOptions
	shellcheckSettings      *ShellcheckSettings
	workingDir, projectRoot string
	reader                  *fsread.Reader
	gitModes                *gitModes
}

func builtinRuleDescriptors() []ruleDescriptor {
	rules := []ruleDescriptor{
		{Name: "workflow-run-names", Description: "Report workflow_run references to unknown repository workflows", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) {
			return &workflowRunNamesRule{RuleBase: builtinRuleBase("workflow-run-names"), root: absPath(c.projectRoot), index: c.workflowNames}, nil
		}, enabled: workflowRunNamesAvailable},
		{Name: "unsound-ternary", Description: "Report a && b || c expressions where b is always falsy", Category: "correctness", Recommended: true},
		{configurationScope: compositeConfiguration, required: func(c ruleContext) bool { return c.ruffOptions == nil || !c.ruffOptions.Optional }, supportsActions: true, Name: "ruff", Description: "Analyze embedded Python scripts with Ruff", Category: "external", Recommended: true, buildScripts: newRuffAnalyzer, enabled: func(c ruleContext) bool {
			possible, err := ruffProjectConfigMayEnable(c.config, c.projectConfig, c.projectRoot != "")
			// Validated config should merge successfully; analysis resolves the
			// actual metadata path and reports any remaining composition error.
			return externalCommandEnabled(c.ruff, c.ruffOptions) && (possible || err != nil)
		}},
		{supportsActions: true, Name: "syntax-check", Description: "Validate GitHub Actions workflow syntax", Category: "correctness", Recommended: true},
		{configurationScope: sourceConfiguration, Name: "inline-suppression", Description: "Validate inline suppression directives", Category: "correctness", Recommended: true},
		{Name: "missing-local-action", Description: "Report local action references whose manifests cannot be found", Category: "correctness", Recommended: true},
		{Name: "string-conditions", Description: "Flag string conditions whose truthiness may be misleading", Category: "suspicious"},
		{Name: "mixed-type-comparisons", Description: "Flag condition comparisons that implicitly convert scalar types", Category: "suspicious"},
		{Name: "case-insensitive-conditions", Description: "Flag case-insensitive identity comparisons", Category: "suspicious"},
		{Name: "mixed-type-matrix-filters", Description: "Flag matrix filters that compare different or unknown scalar types", Category: "suspicious"},
		{configurationScope: sourceConfiguration, Name: "disallow-suppressions", Description: "Report suppressions forbidden by policy", Category: "policy"},
		{Name: "matrix", Description: "Validate matrix definitions, combinations, and filters", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleMatrix(), nil }},
		{Name: "credentials", Description: "Detect hardcoded passwords in job and service containers", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleCredentials(), nil }},
		{Name: "shell-name", Description: "Validate script shells for the selected runner", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleShellName(), nil }},
		{Name: "runner-label", Description: "Report unknown or incompatible runner labels", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleRunnerLabel(), nil }},
		{Name: "events", Description: "Validate workflow triggers and their options", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleEvents(), nil }},
		{Name: "job-needs", Description: "Report undefined job dependencies and dependency cycles", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleJobNeeds(), nil }},
		{Name: "parallel-steps", Description: "Validate background-step references and restrictions within parallel groups", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleParallelSteps(), nil }},
		{supportsActions: true, Name: "action", Description: "Validate action references, inputs, outputs, and local metadata", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleAction(c.actions), nil }},
		{Name: "executable-bit", Description: "Report directly invoked repository scripts not marked executable in Git", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) {
			return newRuleExecutableBit(c), nil
		}, enabled: func(c ruleContext) bool { return c.projectRoot != "" && c.gitModes != nil }},
		{Name: "env-var", Description: "Validate environment variable declarations", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleEnvVar(), nil }},
		{Name: "id", Description: "Report invalid job or step IDs and duplicate step IDs", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleID(), nil }},
		{Name: "glob", Description: "Validate branch, tag, and path filter patterns", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleGlob(), nil }},
		{Name: "permissions", Description: "Validate token permission scopes and access levels", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRulePermissions(), nil }},
		{Name: "workflow-call", Description: "Validate reusable workflow references and their declared inputs and outputs", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleWorkflowCall(c.path, c.workflows), nil }},
		{Name: "expression", Description: "Validate expression syntax, names, types, and context availability", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleExpression(c.actions, c.workflows), nil }},
		{supportsActions: true, configurationScope: sourceConfiguration, Name: "deprecated-commands", Description: "Report deprecated set-output, save-state, set-env, and add-path commands", Category: "correctness", Recommended: true, buildScripts: func(ruleContext) (scriptAnalyzer, error) { return deprecatedCommandsAnalyzer{}, nil }},
		{Name: "if-cond", Description: "Report conditions that always evaluate to true or false", Category: "correctness", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleIfCond(), nil }},
		{Name: "require-commit-hash", Description: "Require full commit SHAs for actions and workflows, and digests for images", Category: "policy", build: func(c ruleContext) (Rule, error) { return NewRuleRequireCommitHash(), nil }, enabled: func(c ruleContext) bool { return c.config.RequiresCommitHash() }},
		{Name: "require-job-timeout", Description: "Require explicit job timeouts within configured bounds", Category: "policy", build: func(c ruleContext) (Rule, error) { return NewRuleRequireJobTimeout(c.config.RequiresJobTimeout()), nil }, enabled: func(c ruleContext) bool { return c.config.RequiresJobTimeout().Enabled() }},
		{Name: "require-permissions", Description: "Require explicit token permissions at workflow or job scope", Category: "policy", build: func(c ruleContext) (Rule, error) {
			return NewRuleRequirePermissions(c.config.RequiresPermissions()), nil
		}, enabled: func(c ruleContext) bool { return c.config.RequiresPermissions().Enabled() }},
		{Name: "required-actions", Description: "Require actions listed in the project policy", Category: "policy", build: func(c ruleContext) (Rule, error) { return NewRuleRequiredActions(), nil }, enabled: func(c ruleContext) bool { return len(c.config.RequiredActions()) > 0 }},
		{Name: "cache-write-untrusted", Description: "Report cache write access granted on low-trust triggers", Category: "policy", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleCacheWriteUntrusted(), nil }, enabled: func(c ruleContext) bool { return c.config.cachePolicyEnabled("cache-write-untrusted") }},
		{Name: "cache-call-unrestricted", Description: "Report reusable workflow calls without explicit cache limits on low-trust triggers", Category: "policy", Recommended: true, build: func(c ruleContext) (Rule, error) { return NewRuleCacheCallUnrestricted(), nil }, enabled: func(c ruleContext) bool { return c.config.cachePolicyEnabled("cache-call-unrestricted") }},
		{Name: "cache-operation", Description: "Report cache actions blocked by explicit cache access limits", Category: "policy", Recommended: true, build: func(c ruleContext) (Rule, error) { return newRuleCacheOperation(c.workflows), nil }, enabled: func(c ruleContext) bool { return c.config.cachePolicyEnabled("cache-operation") }},
		{supportsActions: true, configurationScope: sourceConfiguration, skipWhenDisabled: true, requiresScriptExecution: true, Name: "shellcheck", Description: "Analyze embedded shell scripts with ShellCheck", Category: "external", Recommended: true, buildScripts: newShellcheckAnalyzer, enabled: func(c ruleContext) bool {
			return (c.shellcheckRunner != nil || externalCommandEnabled(c.shellcheck, c.shellcheckOptions)) &&
				(c.config == nil || c.config.Tools.Shellcheck.Enabled == nil || *c.config.Tools.Shellcheck.Enabled)
		}},
	}
	for i := range rules {
		configureRuleDescriptor(&rules[i])
	}
	return rules
}

func findRuleDescriptor(name string) (ruleDescriptor, bool) {
	for _, d := range builtinRuleDescriptors() {
		if d.Name == name {
			return d, true
		}
	}
	return ruleDescriptor{}, false
}

func (d ruleDescriptor) isNursery() bool { return d.Category == "nursery" }

func (d ruleDescriptor) maturity() string {
	if d.isNursery() {
		return "nursery"
	}
	return "stable"
}
func builtinRuleBase(name string) RuleBase {
	for _, descriptor := range builtinRuleDescriptors() {
		if descriptor.Name == name {
			return NewRuleBase(descriptor.Name, descriptor.Description)
		}
	}
	panic("unregistered built-in rule: " + name)
}

// RuleInfo describes an available check without constructing a rule or external process.
type RuleInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Maturity    string `json:"maturity"`
	Recommended bool   `json:"recommended"`
}

// BuiltinRuleOptions returns an empty typed options value for schema generation,
// or nil for a rule without options. Each call returns a fresh value.
func BuiltinRuleOptions(name string) any {
	if d, ok := findRuleDescriptor(name); ok && d.newOptions != nil {
		return d.newOptions()
	}
	return nil
}

// BuiltinRules returns metadata for the registered built-in checks.
func BuiltinRules() []RuleInfo {
	descriptors := builtinRuleDescriptors()
	result := make([]RuleInfo, len(descriptors))
	for i, rule := range descriptors {
		result[i] = RuleInfo{Name: rule.Name, Description: rule.Description, Category: rule.Category, Maturity: rule.maturity(), Recommended: rule.Recommended}
	}
	return result
}
