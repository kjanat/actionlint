package engine

import (
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	repoconfig "actionlint.kjanat.dev/internal/config"
	lintconfig "actionlint.kjanat.dev/internal/config/rules"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflow/calls"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/rule"
)

type ruleConfigurationScope uint8

const (
	callerConfiguration ruleConfigurationScope = iota
	sourceConfiguration
	compositeConfiguration
)

// ruleDescriptor joins a catalog entry with its host execution behaviour.
type ruleDescriptor struct {
	selected bool
	rule.Descriptor
	Group                   string
	requiresScriptExecution bool
	buildScripts            func(ruleContext) (scriptAnalyzer, error)
	configurationScope      ruleConfigurationScope
	skipWhenDisabled        bool
	required                func(ruleContext) bool
	supportsActions         bool
	configure               func(*Config, RuleSetting)
	build                   func(ruleContext) (workflowvisit.Rule, error)
	enabled                 func(ruleContext) bool
}
type ruleContext struct {
	workflowNames           *workflownames.Index
	path                    string
	config                  *Config
	projectConfig           *Config
	actions                 *actionlocal.Resolver
	workflows               *calls.Cache
	process                 *concurrentProcess
	workingDir, projectRoot string
	reader                  *fsread.Reader
	gitModes                *gitModes
}

type ruleBehaviour struct {
	requiresScriptExecution bool
	skipWhenDisabled        bool
	buildScripts            func(ruleContext) (scriptAnalyzer, error)
	required                func(ruleContext) bool
	build                   func(ruleContext) (workflowvisit.Rule, error)
	enabled                 func(ruleContext) bool
}

func ruleBehaviours() map[string]ruleBehaviour {
	return map[string]ruleBehaviour{
		"workflow-run-names": {build: func(c ruleContext) (workflowvisit.Rule, error) {
			return &workflowRunNamesRule{Base: builtinRuleBase("workflow-run-names"), root: absPath(c.projectRoot), index: c.workflowNames}, nil
		}, enabled: workflowRunNamesAvailable},
		"disallow-suppressions": {enabled: func(c ruleContext) bool { return c.config != nil && c.config.Policy.DisallowSuppressions.Enabled() }},
		"matrix":                {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleMatrix(), nil }},
		"credentials":           {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleCredentials(), nil }},
		"shell-name":            {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleShellName(), nil }},
		"runner-label":          {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleRunnerLabel(), nil }},
		"events":                {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleEvents(), nil }},
		"job-needs":             {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleJobNeeds(), nil }},
		"parallel-steps":        {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleParallelSteps(), nil }},
		"action":                {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleAction(c.actions), nil }},
		"executable-bit": {build: func(c ruleContext) (workflowvisit.Rule, error) {
			return newRuleExecutableBit(c), nil
		}, enabled: func(c ruleContext) bool { return c.projectRoot != "" && c.gitModes != nil }},
		"env-var":             {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleEnvVar(), nil }},
		"id":                  {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleID(), nil }},
		"glob":                {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleGlob(), nil }},
		"permissions":         {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRulePermissions(), nil }},
		"workflow-call":       {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleWorkflowCall(c.path, c.workflows), nil }},
		"expression":          {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleExpression(c.actions, c.workflows), nil }},
		"deprecated-commands": {buildScripts: func(ruleContext) (scriptAnalyzer, error) { return deprecatedCommandsAnalyzer{}, nil }},
		"if-cond":             {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleIfCond(), nil }},
		"require-commit-hash": {build: func(c ruleContext) (workflowvisit.Rule, error) {
			var options *lintconfig.CommitHashOptions
			if c.config != nil {
				options, _ = c.config.Lint.Rules.Policy.Rules["require-commit-hash"].Options.(*lintconfig.CommitHashOptions)
			}
			return NewRuleRequireCommitHash(options.Excludes), nil
		}, enabled: func(c ruleContext) bool { return c.config.RequiresCommitHash() }},
		"require-job-timeout": {build: func(c ruleContext) (workflowvisit.Rule, error) {
			return NewRuleRequireJobTimeout(c.config.RequiresJobTimeout()), nil
		}, enabled: func(c ruleContext) bool { return c.config.RequiresJobTimeout().Enabled() }},
		"require-permissions": {build: func(c ruleContext) (workflowvisit.Rule, error) {
			return NewRuleRequirePermissions(c.config.RequiresPermissions()), nil
		}, enabled: func(c ruleContext) bool { return c.config.RequiresPermissions().Enabled() }},
		"required-actions": {build: func(c ruleContext) (workflowvisit.Rule, error) {
			return NewRuleRequiredActions(c.config.RequiredActions()), nil
		}, enabled: func(c ruleContext) bool { return len(c.config.RequiredActions()) > 0 }},
		"cache-write-untrusted":   {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleCacheWriteUntrusted(), nil }, enabled: func(c ruleContext) bool { return c.config.CachePolicyEnabled("cache-write-untrusted") }},
		"cache-call-unrestricted": {build: func(c ruleContext) (workflowvisit.Rule, error) { return NewRuleCacheCallUnrestricted(), nil }, enabled: func(c ruleContext) bool { return c.config.CachePolicyEnabled("cache-call-unrestricted") }},
		"cache-operation":         {build: func(c ruleContext) (workflowvisit.Rule, error) { return newRuleCacheOperation(c.workflows), nil }, enabled: func(c ruleContext) bool { return c.config.CachePolicyEnabled("cache-operation") }},
	}
}

func builtinRuleDescriptors() []ruleDescriptor {
	behaviours := ruleBehaviours()
	catalog := repoconfig.Rules()
	rules := make([]ruleDescriptor, len(catalog))
	for i, rule := range catalog {
		behaviour := behaviours[rule.Name]
		rules[i] = ruleDescriptor{
			requiresScriptExecution: behaviour.requiresScriptExecution,
			buildScripts:            behaviour.buildScripts,
			configurationScope:      configurationScopeOf(rule.Scope),
			skipWhenDisabled:        behaviour.skipWhenDisabled,
			required:                behaviour.required,
			supportsActions:         rule.SupportsActions,
			Descriptor:              rule.Descriptor, Group: rule.Group,
			configure: rule.Configure,
			build:     behaviour.build,
			enabled:   behaviour.enabled,
		}
	}
	return rules
}

func configurationScopeOf(scope repoconfig.Scope) ruleConfigurationScope {
	switch scope {
	case repoconfig.SourceScope:
		return sourceConfiguration
	case repoconfig.CompositeScope:
		return compositeConfiguration
	default:
		return callerConfiguration
	}
}

func findRuleDescriptor(name string) (ruleDescriptor, bool) {
	for _, d := range builtinRuleDescriptors() {
		if d.Name == name {
			return d, true
		}
	}
	return ruleDescriptor{}, false
}

func (d ruleDescriptor) isNursery() bool { return d.Maturity == "nursery" }

func (d ruleDescriptor) maturity() string {
	if d.isNursery() {
		return "nursery"
	}
	return "stable"
}
func builtinRuleBase(name string) workflowvisit.Base {
	for _, descriptor := range builtinRuleDescriptors() {
		if descriptor.Name == name {
			return workflowvisit.NewBase(descriptor.Name, descriptor.Description)
		}
	}
	panic("unregistered built-in rule: " + name)
}

// RuleInfo describes an available check without constructing a rule or external process.
type RuleInfo = repoconfig.RuleInfo

// BuiltinRules returns metadata for the registered built-in checks.
func BuiltinRules() []RuleInfo { return repoconfig.RuleInfos() }
