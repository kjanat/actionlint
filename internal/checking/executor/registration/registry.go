package registration

import (
	"context"
	"path/filepath"

	expressionchecks "actionlint.kjanat.dev/v2/internal/checking/executor/expressions"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"

	workflowcommands "actionlint.kjanat.dev/v2/internal/checking/commands"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	"actionlint.kjanat.dev/v2/internal/input/files"

	workflownames "actionlint.kjanat.dev/v2/internal/input/files/names"

	actionlocal "actionlint.kjanat.dev/v2/internal/model/actions/local"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	repository "actionlint.kjanat.dev/v2/internal/model/repositories"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

// Inputs supplies captured sources, resolved settings and model facts to rule constructors.
// It contains no process pool, application session or engine implementation.
// Context controls cancellation for rule work, including filesystem reads.
type Inputs struct {
	Rules                         repoconfig.RuleSet
	SourceSnapshot                parsing.SourceSnapshot
	Runners                       *runnerlang.Knowledge
	Context                       context.Context
	Path, WorkingDir, ProjectRoot string
	Config                        *repoconfig.Config
	Repository                    repository.Snapshot
	Actions                       *actionlocal.Resolver
	RemoteActions                 *actionremote.ResolvedActions
	Workflows                     *calls.Cache
	WorkflowNames                 *workflownames.Index
	Reader                        *files.Reader
	Index                         func(context.Context, string) *execution.IndexSnapshot
}

// Registration pairs a rule declaration with its constructor and execution conditions.
type Registration struct {
	repoconfig.Rule
	Build   func(Inputs) workflowvisit.Rule
	Script  checkinputs.Checker
	Enabled func(Inputs) bool
}

type implementation struct {
	Build   func(Inputs) workflowvisit.Rule
	Script  checkinputs.Checker
	Enabled func(Inputs) bool
}

func ruleImplementations() map[string]implementation {
	return map[string]implementation{
		"missing-action-metadata": {Build: func(c Inputs) workflowvisit.Rule {
			return conventions.NewRuleRequireActionMetadata(c.Context, c.RemoteActions)
		}, Enabled: func(c Inputs) bool {
			return c.Config != nil && c.Config.Policy.RequireActionMetadata != nil && *c.Config.Policy.RequireActionMetadata
		}},
		"workflow-run-names": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleWorkflowRunNames(absolutePath(c.ProjectRoot), c.WorkflowNames, c.Config.RuleSet().DiagnosticLevel(c.Config, "workflow-run-names") != "off")
		}, Enabled: workflowRunNamesAvailable},
		"matrix": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleMatrix(c.Config.RuleSet().DiagnosticLevel(c.Config, "mixed-type-matrix-filters") != "off")
		}},
		"credentials": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "credentials") }},
		"shell-name":  {Build: func(c Inputs) workflowvisit.Rule { return correctness.NewRuleShellName(c.Runners) }},
		"runner-label": {Build: func(c Inputs) workflowvisit.Rule {
			labels := runnerlang.LabelSet{}
			if known, available := repoconfig.RepositoryFacts(c.Config, c.Repository).RunnerLabels.Value(); available {
				labels = runnerlang.KnownLabels(known.Values())
			}
			return correctness.NewRuleRunnerLabel(c.Runners, labels)
		}},
		"container-runner": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleContainerRunner(c.Context, c.Runners, c.Actions, c.RemoteActions)
		}},
		"vendor-lock-in": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.NewVendorLockIn(c.Rules, c.Runners) }},
		"events":         {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "events") }},
		"job-needs":      {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "job-needs") }},
		"parallel-steps": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "parallel-steps") }},
		"action": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleAction(c.Context, c.Actions, c.RemoteActions)
		}},
		"executable-bit": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleExecutableBit(correctness.ExecutableContext{
				Context: c.Context, ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
				Reader: c.Reader, Index: c.Index, Runners: c.Runners,
			})
		}, Enabled: func(c Inputs) bool { return c.ProjectRoot != "" && c.Index != nil }},
		"env-var":     {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "env-var") }},
		"id":          {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "id") }},
		"glob":        {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "glob") }},
		"permissions": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "permissions") }},
		"missing-workflow-permissions": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, "missing-workflow-permissions")
		}},
		"workflow-call": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleWorkflowCall(c.SourceSnapshot, c.Workflows, repoconfig.RepositoryFacts(c.Config, c.Repository))
		}},
		"expression": {Build: func(c Inputs) workflowvisit.Rule {
			return expressionchecks.New(c.Context, c.Actions, c.Workflows, c.Runners, c.RemoteActions)
		}},
		"deprecated-commands": {Script: workflowcommands.Checker{}},
		"if-cond":             {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "if-cond") }},
		"uncancellable-condition": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, "uncancellable-condition")
		}},
		"numeric-values": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "numeric-values") }},
		"empty-triggers": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "empty-triggers") }},
		"empty-job":      {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "empty-job") }},
		"empty-run":      {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, "empty-run") }},
		"unpinned-reference": {Build: func(c Inputs) workflowvisit.Rule {
			options := new(lintconfig.CommitHashOptions)
			if c.Config != nil {
				// Parsing and ValidateRuleNames reject options that do not load
				// here; without that check they leave no exclusions.
				setting, _ := c.Config.Lint.Rules.Policy.Rule("unpinned-reference")
				if _, err := setting.LoadOptions(options); err != nil {
					options = new(lintconfig.CommitHashOptions)
				}
			}
			return declarationchecks.NewRuleRequireCommitHash(c.Rules, options.Excludes)
		}, Enabled: func(c Inputs) bool { return c.Config.RequiresCommitHash() }},
		"job-timeout": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.NewRuleRequireJobTimeout(c.Rules, c.Config.RequiresJobTimeout())
		}, Enabled: func(c Inputs) bool { return c.Config.RequiresJobTimeout().Enabled() }},
		"explicit-permissions": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.NewRuleRequirePermissions(c.Rules, c.Config.RequiresPermissions())
		}, Enabled: func(c Inputs) bool { return c.Config.RequiresPermissions().Enabled() }},
		"cache-write-untrusted":   {Build: func(c Inputs) workflowvisit.Rule { return security.NewRuleCacheWriteUntrusted() }, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-write-untrusted") }},
		"cache-call-unrestricted": {Build: func(c Inputs) workflowvisit.Rule { return security.NewRuleCacheCallUnrestricted() }, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") }},
		"cache-operation":         {Build: func(c Inputs) workflowvisit.Rule { return correctness.NewRuleCacheOperation(c.Workflows) }, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-operation") }},
	}
}

// Registrations returns check implementations in rule-declaration order.
func Registrations(rules repoconfig.RuleSet) []Registration {
	behaviors := ruleImplementations()
	for name := range behaviors {
		if _, found := rules.FindRule(name); !found {
			panic("check implementation " + name + " has no rule declaration")
		}
	}
	catalog := rules.Rules()
	out := make([]Registration, len(catalog))
	for i, metadata := range catalog {
		behavior := behaviors[metadata.Name]
		out[i] = Registration{Rule: metadata, Build: behavior.Build, Script: behavior.Script, Enabled: behavior.Enabled}
	}
	return out
}

// HasRule reports whether a check implementation uses this registration name.
func HasRule(name string) bool {
	behavior := ruleImplementations()[name]
	return behavior.Build != nil || behavior.Script != nil
}

func absolutePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func workflowRunNamesAvailable(c Inputs) bool {
	if c.ProjectRoot == "" || c.WorkflowNames == nil {
		return false
	}
	path := c.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.WorkingDir, path)
	}
	return workflownames.SamePath(filepath.Dir(path), filepath.Join(absolutePath(c.ProjectRoot), ".github", "workflows"))
}
