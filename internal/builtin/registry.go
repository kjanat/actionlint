package builtin

import (
	"context"
	"path/filepath"

	"actionlint.kjanat.dev/v2/analyzer"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	actionremote "actionlint.kjanat.dev/v2/internal/action/remote"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	lintconfig "actionlint.kjanat.dev/v2/internal/config/rules"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowcommands "actionlint.kjanat.dev/v2/internal/workflow/commands"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflownames "actionlint.kjanat.dev/v2/internal/workflow/names"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/repository"
)

// RuleInputs supplies resolved document inputs and observations to built-in adapters.
// It contains no process pool, application session or engine implementation.
// Context controls cancellation for rule work, including filesystem reads.
type RuleInputs struct {
	SourceSnapshot                documentparse.SourceSnapshot
	Runners                       *runnerlang.Knowledge
	Context                       context.Context
	Path, WorkingDir, ProjectRoot string
	Config                        *repoconfig.Config
	Repository                    repository.Snapshot
	Actions                       *actionlocal.Resolver
	RemoteActions                 *actionremote.Service
	Workflows                     *calls.Cache
	WorkflowNames                 *workflownames.Index
	Reader                        *fsread.Reader
	Index                         func(context.Context, string) *execution.IndexSnapshot
}

// Descriptor joins catalogue metadata with a workflow visitor or a script fact analyzer.
type Descriptor struct {
	repoconfig.Rule
	Build   func(RuleInputs) workflowvisit.Rule
	Script  analyzer.Checker
	Enabled func(RuleInputs) bool
}

type behaviour struct {
	Build   func(RuleInputs) workflowvisit.Rule
	Script  analyzer.Checker
	Enabled func(RuleInputs) bool
}

func ruleBehaviours() map[string]behaviour {
	return map[string]behaviour{
		"missing-action-metadata": {Build: func(c RuleInputs) workflowvisit.Rule { return newRuleRequireActionMetadata(c.Context, c.RemoteActions) }, Enabled: func(c RuleInputs) bool {
			return c.Config != nil && c.Config.Policy.RequireActionMetadata != nil && *c.Config.Policy.RequireActionMetadata
		}},
		"workflow-run-names": {Build: func(c RuleInputs) workflowvisit.Rule {
			return &workflowRunNamesRule{Base: builtinRuleBase("workflow-run-names"), root: absolutePath(c.ProjectRoot), index: c.WorkflowNames}
		}, Enabled: workflowRunNamesAvailable},
		"matrix":           {Build: func(RuleInputs) workflowvisit.Rule { return NewRuleMatrix() }},
		"credentials":      {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleCredentials() }},
		"shell-name":       {Build: func(c RuleInputs) workflowvisit.Rule { return newRuleShellName(c.Runners) }},
		"runner-label":     {Build: func(c RuleInputs) workflowvisit.Rule { return newRuleRunnerLabel(c.Runners) }},
		"container-runner": {Build: func(c RuleInputs) workflowvisit.Rule { return newRuleContainerRunner(c) }},
		"vendor-lock-in":   {Build: func(c RuleInputs) workflowvisit.Rule { return newRuleVendorLockIn(c.Runners) }},
		"events":           {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleEvents() }},
		"job-needs":        {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleJobNeeds() }},
		"parallel-steps":   {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleParallelSteps() }},
		"action": {Build: func(c RuleInputs) workflowvisit.Rule {
			rule := newRuleAction(c.Actions)
			rule.remote, rule.context = c.RemoteActions, c.Context
			return rule
		}},
		"executable-bit": {Build: func(c RuleInputs) workflowvisit.Rule {
			return newRuleExecutableBit(c)
		}, Enabled: func(c RuleInputs) bool { return c.ProjectRoot != "" && c.Index != nil }},
		"env-var":     {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleEnvVar() }},
		"id":          {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleID() }},
		"glob":        {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleGlob() }},
		"permissions": {Build: func(c RuleInputs) workflowvisit.Rule { return NewRulePermissions() }},
		"missing-workflow-permissions": {Build: func(RuleInputs) workflowvisit.Rule {
			return newDeclarationRule("missing-workflow-permissions")
		}},
		"workflow-call": {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleWorkflowCall(c.SourceSnapshot, c.Workflows) }},
		"expression": {Build: func(c RuleInputs) workflowvisit.Rule {
			return newRuleExpressionWithRemote(c.Actions, c.Workflows, c.Runners, c.RemoteActions, c.Context)
		}},
		"deprecated-commands": {Script: workflowcommands.Checker{}},
		"if-cond":             {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleIfCond() }},
		"uncancellable-condition": {Build: func(RuleInputs) workflowvisit.Rule {
			return newDeclarationRule("uncancellable-condition")
		}},
		"numeric-values": {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleNumericValues() }},
		"empty-triggers": {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleEmptyTriggers() }},
		"empty-job":      {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleEmptyJob() }},
		"empty-run":      {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleEmptyRun() }},
		"unpinned-reference": {Build: func(c RuleInputs) workflowvisit.Rule {
			options := new(lintconfig.CommitHashOptions)
			if c.Config != nil {
				// Parsing and ValidateRuleNames reject options that do not load
				// here; without that check they leave no exclusions.
				setting, _ := c.Config.Lint.Rules.Policy.Rule("unpinned-reference")
				if _, err := setting.LoadOptions(options); err != nil {
					options = new(lintconfig.CommitHashOptions)
				}
			}
			return NewRuleRequireCommitHash(options.Excludes)
		}, Enabled: func(c RuleInputs) bool { return c.Config.RequiresCommitHash() }},
		"job-timeout": {Build: func(c RuleInputs) workflowvisit.Rule {
			return NewRuleRequireJobTimeout(c.Config.RequiresJobTimeout())
		}, Enabled: func(c RuleInputs) bool { return c.Config.RequiresJobTimeout().Enabled() }},
		"explicit-permissions": {Build: func(c RuleInputs) workflowvisit.Rule {
			return NewRuleRequirePermissions(c.Config.RequiresPermissions())
		}, Enabled: func(c RuleInputs) bool { return c.Config.RequiresPermissions().Enabled() }},
		"cache-write-untrusted":   {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleCacheWriteUntrusted() }, Enabled: func(c RuleInputs) bool { return c.Config.CachePolicyEnabled("cache-write-untrusted") }},
		"cache-call-unrestricted": {Build: func(c RuleInputs) workflowvisit.Rule { return NewRuleCacheCallUnrestricted() }, Enabled: func(c RuleInputs) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") }},
		"cache-operation":         {Build: func(c RuleInputs) workflowvisit.Rule { return newRuleCacheOperation(c.Workflows) }, Enabled: func(c RuleInputs) bool { return c.Config.CachePolicyEnabled("cache-operation") }},
	}
}

// Descriptors returns built-in composition in catalogue order.
func Descriptors() []Descriptor {
	behaviors := ruleBehaviours()
	catalog := repoconfig.Rules()
	out := make([]Descriptor, len(catalog))
	for i, metadata := range catalog {
		behavior := behaviors[metadata.Name]
		out[i] = Descriptor{Rule: metadata, Build: behavior.Build, Script: behavior.Script, Enabled: behavior.Enabled}
	}
	return out
}

// Owns reports whether a built-in analyzer reserves this registration name.
func Owns(name string) bool {
	behavior := ruleBehaviours()[name]
	return behavior.Build != nil || behavior.Script != nil
}

func builtinRuleBase(name string) workflowvisit.Base {
	if metadata, ok := repoconfig.FindRule(name); ok {
		return workflowvisit.NewBase(name, metadata.Description)
	}
	panic("unregistered built-in rule: " + name)
}

func builtinRuleDescription(name string) string {
	base := builtinRuleBase(name)
	return base.Description()
}

func absolutePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}
