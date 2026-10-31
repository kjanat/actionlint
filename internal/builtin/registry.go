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

// Context supplies resolved document inputs and observations to built-in adapters.
// It contains no process pool, application session or engine implementation.
// Context is the analysis lifetime; rules that read the filesystem require it.
type Context struct {
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
	Build   func(Context) workflowvisit.Rule
	Script  analyzer.Analyzer
	Enabled func(Context) bool
}

type behaviour struct {
	Build   func(Context) workflowvisit.Rule
	Script  analyzer.Analyzer
	Enabled func(Context) bool
}

func ruleBehaviours() map[string]behaviour {
	return map[string]behaviour{
		"workflow-run-names": {Build: func(c Context) workflowvisit.Rule {
			return &workflowRunNamesRule{Base: builtinRuleBase("workflow-run-names"), root: absolutePath(c.ProjectRoot), index: c.WorkflowNames}
		}, Enabled: workflowRunNamesAvailable},
		"matrix":         {Build: func(Context) workflowvisit.Rule { return NewRuleMatrix() }},
		"credentials":    {Build: func(c Context) workflowvisit.Rule { return NewRuleCredentials() }},
		"shell-name":     {Build: func(c Context) workflowvisit.Rule { return newRuleShellName(c.Runners) }},
		"runner-label":   {Build: func(c Context) workflowvisit.Rule { return newRuleRunnerLabel(c.Runners) }},
		"events":         {Build: func(c Context) workflowvisit.Rule { return NewRuleEvents() }},
		"job-needs":      {Build: func(c Context) workflowvisit.Rule { return NewRuleJobNeeds() }},
		"parallel-steps": {Build: func(c Context) workflowvisit.Rule { return NewRuleParallelSteps() }},
		"action": {Build: func(c Context) workflowvisit.Rule {
			rule := newRuleAction(c.Actions)
			rule.remote, rule.context = c.RemoteActions, c.Context
			return rule
		}},
		"executable-bit": {Build: func(c Context) workflowvisit.Rule {
			return newRuleExecutableBit(c)
		}, Enabled: func(c Context) bool { return c.ProjectRoot != "" && c.Index != nil }},
		"env-var":       {Build: func(c Context) workflowvisit.Rule { return NewRuleEnvVar() }},
		"id":            {Build: func(c Context) workflowvisit.Rule { return NewRuleID() }},
		"glob":          {Build: func(c Context) workflowvisit.Rule { return NewRuleGlob() }},
		"permissions":   {Build: func(c Context) workflowvisit.Rule { return NewRulePermissions() }},
		"workflow-call": {Build: func(c Context) workflowvisit.Rule { return NewRuleWorkflowCall(c.SourceSnapshot, c.Workflows) }},
		"expression": {Build: func(c Context) workflowvisit.Rule {
			return newRuleExpressionWithRemote(c.Actions, c.Workflows, c.Runners, c.RemoteActions, c.Context)
		}},
		"deprecated-commands": {Script: workflowcommands.Analyzer{}},
		"if-cond":             {Build: func(c Context) workflowvisit.Rule { return NewRuleIfCond() }},
		"numeric-values":      {Build: func(c Context) workflowvisit.Rule { return NewRuleNumericValues() }},
		"empty-triggers":      {Build: func(c Context) workflowvisit.Rule { return NewRuleEmptyTriggers() }},
		"empty-job":           {Build: func(c Context) workflowvisit.Rule { return NewRuleEmptyJob() }},
		"empty-run":           {Build: func(c Context) workflowvisit.Rule { return NewRuleEmptyRun() }},
		"require-commit-hash": {Build: func(c Context) workflowvisit.Rule {
			options := new(lintconfig.CommitHashOptions)
			if c.Config != nil {
				// Parsing and ValidateRuleNames reject options that do not load
				// here; without that check they leave no exclusions.
				setting, _ := c.Config.Lint.Rules.Policy.Rule("require-commit-hash")
				if _, err := setting.LoadOptions(options); err != nil {
					options = new(lintconfig.CommitHashOptions)
				}
			}
			return NewRuleRequireCommitHash(options.Excludes)
		}, Enabled: func(c Context) bool { return c.Config.RequiresCommitHash() }},
		"require-job-timeout": {Build: func(c Context) workflowvisit.Rule {
			return NewRuleRequireJobTimeout(c.Config.RequiresJobTimeout())
		}, Enabled: func(c Context) bool { return c.Config.RequiresJobTimeout().Enabled() }},
		"require-permissions": {Build: func(c Context) workflowvisit.Rule {
			return NewRuleRequirePermissions(c.Config.RequiresPermissions())
		}, Enabled: func(c Context) bool { return c.Config.RequiresPermissions().Enabled() }},
		"required-actions": {Build: func(c Context) workflowvisit.Rule {
			return NewRuleRequiredActions(c.Config.RequiredActions())
		}, Enabled: func(c Context) bool { return len(c.Config.RequiredActions()) > 0 }},
		"cache-write-untrusted":   {Build: func(c Context) workflowvisit.Rule { return NewRuleCacheWriteUntrusted() }, Enabled: func(c Context) bool { return c.Config.CachePolicyEnabled("cache-write-untrusted") }},
		"cache-call-unrestricted": {Build: func(c Context) workflowvisit.Rule { return NewRuleCacheCallUnrestricted() }, Enabled: func(c Context) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") }},
		"cache-operation":         {Build: func(c Context) workflowvisit.Rule { return newRuleCacheOperation(c.Workflows) }, Enabled: func(c Context) bool { return c.Config.CachePolicyEnabled("cache-operation") }},
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
