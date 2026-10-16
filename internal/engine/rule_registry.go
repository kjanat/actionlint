package engine

import (
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/repoconfig"
	"actionlint.kjanat.dev/internal/ruff"
	"actionlint.kjanat.dev/internal/workflownames"
)

type ruleConfigurationScope uint8

const (
	callerConfiguration ruleConfigurationScope = iota
	sourceConfiguration
	compositeConfiguration
)

// ruleDescriptor joins a catalog entry with its host execution behaviour.
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
	ruffBackend             analyzer.ScriptBackend
	shellcheckBackend       analyzer.ScriptBackend
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

type ruleBehaviour struct {
	requiresScriptExecution bool
	skipWhenDisabled        bool
	buildScripts            func(ruleContext) (scriptAnalyzer, error)
	required                func(ruleContext) bool
	build                   func(ruleContext) (Rule, error)
	enabled                 func(ruleContext) bool
}

func ruleBehaviours() map[string]ruleBehaviour {
	return map[string]ruleBehaviour{
		"workflow-run-names": {build: func(c ruleContext) (Rule, error) {
			return &workflowRunNamesRule{RuleBase: builtinRuleBase("workflow-run-names"), root: absPath(c.projectRoot), index: c.workflowNames}, nil
		}, enabled: workflowRunNamesAvailable},
		"ruff": {required: func(c ruleContext) bool {
			options := projectToolOptions("ruff", c.ruff, c.ruffOptions, c.config, c.workingDir)
			return c.ruffBackend != nil || options == nil || !options.Optional
		}, buildScripts: newRuffAnalyzer, enabled: func(c ruleContext) bool {
			possible, err := ruffProjectConfigMayEnable(c.config, c.projectConfig, c.projectRoot != "")
			return (c.ruffBackend != nil || externalCommandEnabled(c.ruff, c.ruffOptions)) && (possible || err != nil)
		}},
		"disallow-suppressions": {enabled: func(c ruleContext) bool { return c.config != nil && c.config.Policy.DisallowSuppressions.Enabled() }},
		"matrix":                {build: func(c ruleContext) (Rule, error) { return NewRuleMatrix(), nil }},
		"credentials":           {build: func(c ruleContext) (Rule, error) { return NewRuleCredentials(), nil }},
		"shell-name":            {build: func(c ruleContext) (Rule, error) { return NewRuleShellName(), nil }},
		"runner-label":          {build: func(c ruleContext) (Rule, error) { return NewRuleRunnerLabel(), nil }},
		"events":                {build: func(c ruleContext) (Rule, error) { return NewRuleEvents(), nil }},
		"job-needs":             {build: func(c ruleContext) (Rule, error) { return NewRuleJobNeeds(), nil }},
		"parallel-steps":        {build: func(c ruleContext) (Rule, error) { return NewRuleParallelSteps(), nil }},
		"action":                {build: func(c ruleContext) (Rule, error) { return NewRuleAction(c.actions), nil }},
		"executable-bit": {build: func(c ruleContext) (Rule, error) {
			return newRuleExecutableBit(c), nil
		}, enabled: func(c ruleContext) bool { return c.projectRoot != "" && c.gitModes != nil }},
		"env-var":             {build: func(c ruleContext) (Rule, error) { return NewRuleEnvVar(), nil }},
		"id":                  {build: func(c ruleContext) (Rule, error) { return NewRuleID(), nil }},
		"glob":                {build: func(c ruleContext) (Rule, error) { return NewRuleGlob(), nil }},
		"permissions":         {build: func(c ruleContext) (Rule, error) { return NewRulePermissions(), nil }},
		"workflow-call":       {build: func(c ruleContext) (Rule, error) { return NewRuleWorkflowCall(c.path, c.workflows), nil }},
		"expression":          {build: func(c ruleContext) (Rule, error) { return NewRuleExpression(c.actions, c.workflows), nil }},
		"deprecated-commands": {buildScripts: func(ruleContext) (scriptAnalyzer, error) { return deprecatedCommandsAnalyzer{}, nil }},
		"if-cond":             {build: func(c ruleContext) (Rule, error) { return NewRuleIfCond(), nil }},
		"require-commit-hash": {build: func(c ruleContext) (Rule, error) { return NewRuleRequireCommitHash(), nil }, enabled: func(c ruleContext) bool { return c.config.RequiresCommitHash() }},
		"require-job-timeout": {build: func(c ruleContext) (Rule, error) { return NewRuleRequireJobTimeout(c.config.RequiresJobTimeout()), nil }, enabled: func(c ruleContext) bool { return c.config.RequiresJobTimeout().Enabled() }},
		"require-permissions": {build: func(c ruleContext) (Rule, error) {
			return NewRuleRequirePermissions(c.config.RequiresPermissions()), nil
		}, enabled: func(c ruleContext) bool { return c.config.RequiresPermissions().Enabled() }},
		"required-actions":        {build: func(c ruleContext) (Rule, error) { return NewRuleRequiredActions(), nil }, enabled: func(c ruleContext) bool { return len(c.config.RequiredActions()) > 0 }},
		"cache-write-untrusted":   {build: func(c ruleContext) (Rule, error) { return NewRuleCacheWriteUntrusted(), nil }, enabled: func(c ruleContext) bool { return c.config.CachePolicyEnabled("cache-write-untrusted") }},
		"cache-call-unrestricted": {build: func(c ruleContext) (Rule, error) { return NewRuleCacheCallUnrestricted(), nil }, enabled: func(c ruleContext) bool { return c.config.CachePolicyEnabled("cache-call-unrestricted") }},
		"cache-operation":         {build: func(c ruleContext) (Rule, error) { return newRuleCacheOperation(c.workflows), nil }, enabled: func(c ruleContext) bool { return c.config.CachePolicyEnabled("cache-operation") }},
		"shellcheck": {required: func(c ruleContext) bool {
			options := projectToolOptions("shellcheck", c.shellcheck, c.shellcheckOptions, c.config, c.workingDir)
			return c.shellcheckBackend != nil || options != nil && !options.Optional
		}, skipWhenDisabled: true, requiresScriptExecution: true, buildScripts: newShellcheckAnalyzer, enabled: func(c ruleContext) bool {
			return (c.shellcheckBackend != nil || c.shellcheckRunner != nil || externalCommandEnabled(c.shellcheck, c.shellcheckOptions)) &&
				(c.config == nil || c.config.Tools.Shellcheck.Enabled == nil || *c.config.Tools.Shellcheck.Enabled)
		}},
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
			Name:                    rule.Name,
			Description:             rule.Description,
			Category:                rule.Category,
			Recommended:             rule.Recommended,
			newOptions:              rule.Options,
			configure:               rule.Configure,
			build:                   behaviour.build,
			enabled:                 behaviour.enabled,
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
