package registration

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	expressionchecks "actionlint.kjanat.dev/v2/internal/checking/executor/expressions"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	namecheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflow-run-names"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"

	workflowcommands "actionlint.kjanat.dev/v2/internal/checking/commands"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/runtimechecks"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	"actionlint.kjanat.dev/v2/internal/input/files"

	workflownames "actionlint.kjanat.dev/v2/internal/input/files/names"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	repository "actionlint.kjanat.dev/v2/internal/model/repositories"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

// Inputs supplies captured sources, resolved settings and model facts to rule constructors.
// It contains no process pool, application session or engine implementation.
// Context controls cancellation for rule work, including filesystem reads.
type Inputs struct {
	RequiredPermissionLocations []repoconfig.PermissionLocation

	Debug                         io.Writer
	Rules                         repoconfig.RuleSet
	SourceSnapshot                parsing.SourceSnapshot
	Runners                       *runnerlang.Knowledge
	Context                       context.Context
	Path, WorkingDir, ProjectRoot string
	Config                        *repoconfig.Config
	Repository                    repository.Snapshot
	Actions                       *workspaceactions.Resolver
	RemoteActions                 *actionremote.ResolvedActions
	WorkflowSources               *workflowinputs.Cache
	Workflows                     *workflowinputs.ResolvedWorkflows
	WorkflowNames                 *workflownames.Index
	WorkflowNameInventory         namemodel.Inventory
	Reader                        *files.Reader
	Index                         func(context.Context, string) *execution.IndexSnapshot
	ExecutableIndex               *execution.IndexSnapshot
}

// Registration pairs a rule declaration with its constructor and execution conditions.
type Registration struct {
	repoconfig.Rule
	// Prepare distinguishes usable inputs, unavailable knowledge and operation
	// failures before construction. Nil means no preparation is needed.
	Prepare func(Inputs) (InputPreparation, error)
	Build   func(Inputs) workflowvisit.Rule
	Script  checkinputs.Checker
	Enabled func(Inputs) bool
}

type implementation struct {
	Prepare func(Inputs) (InputPreparation, error)
	Build   func(Inputs) workflowvisit.Rule
	Script  checkinputs.Checker
	Enabled func(Inputs) bool
}

var ruleImplementations = sync.OnceValue(func() map[string]implementation {
	return map[string]implementation{
		"missing-action-metadata": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, conventions.MissingActionMetadataCheck(c.RemoteActions))
		}, Enabled: func(c Inputs) bool {
			return c.Config != nil && c.Config.Policy.RequireActionMetadata != nil && *c.Config.Policy.RequireActionMetadata
		}},
		"workflow-run-names": {Prepare: prepareWorkflowRunNames, Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.WorkflowRunNamesCheck(c.WorkflowNameInventory))
		}, Enabled: workflowRunNamesAvailable},
		"matrix": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.MatrixCheck(c.Config.RuleSet().DiagnosticLevel(c.Config, "mixed-type-matrix-filters") != "off"))
		}},
		"credentials": declarationImplementation(security.CredentialsCheck()),
		"shell-name": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.ShellNameCheck(c.Runners, commands.FromContext(c.Context)))
		}},
		"runner-label": {Build: func(c Inputs) workflowvisit.Rule {
			labels := runnerlang.LabelSet{}
			if known, available := repoconfig.RepositoryFacts(c.Config, c.Repository).RunnerLabels.Value(); available {
				labels = runnerlang.KnownLabels(known.Values())
			}
			return declarationchecks.New(c.Rules, correctness.RunnerLabelCheck(c.Runners, labels))
		}},
		"container-runner": {Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.NewContainerRunner(c.Context, c.Runners, c.Actions, c.RemoteActions)
		}},
		"vendor-lock-in": {Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.NewVendorLockIn(c.Rules, c.Runners) }},
		"events":         declarationImplementation(correctness.EventsCheck()),
		"job-needs":      declarationImplementation(correctness.JobNeedsCheck()),
		"parallel-steps": declarationImplementation(correctness.ParallelStepsCheck()),
		"action": {Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleAction(c.Context, c.Actions.Prepared(), c.RemoteActions)
		}},
		"executable-bit": {Prepare: prepareExecutableIndex, Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleExecutableBit(correctness.ExecutableContext{
				ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
				Index: c.ExecutableIndex, Runners: c.Runners,
			})
		}, Enabled: func(c Inputs) bool { return c.ProjectRoot != "" && c.Index != nil }},
		"env-var":     declarationImplementation(correctness.EnvVarCheck()),
		"id":          declarationImplementation(correctness.IdCheck()),
		"glob":        declarationImplementation(correctness.GlobCheck()),
		"permissions": declarationImplementation(correctness.PermissionsCheck()),
		"missing-permissions": {Prepare: preparePermissionRequirements, Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, security.MissingPermissionsCheck(c.RequiredPermissionLocations))
		}},
		"workflow-call": {Prepare: prepareWorkflowMetadata, Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			logger := reporting.Logger{Tag: "workflow-call", Writer: c.Debug}
			return declarationchecks.New(c.Rules, correctness.WorkflowCallCheck(workflow, c.Workflows, repoconfig.RepositoryFacts(c.Config, c.Repository), logger.Debug))
		}},
		"expression": {Prepare: prepareWorkflowMetadata, Build: func(c Inputs) workflowvisit.Rule {
			return expressionchecks.New(c.Context, c.Actions.Prepared(), c.Workflows, c.Runners, c.RemoteActions)
		}},
		"deprecated-commands":     {Script: workflowcommands.Checker{}},
		"errexit-status":          {Script: runtimechecks.Errexit()},
		"workflow-command-syntax": {Script: runtimechecks.WorkflowCommand()},
		"invalid-output-file":     {Script: correctness.InvalidOutputFileChecker{}},
		"if-cond":                 declarationImplementation(correctness.IfCondCheck()),
		"uncancellable-condition": declarationImplementation(suspicious.UncancellableConditionCheck()),
		"numeric-values":          declarationImplementation(correctness.NumericValuesCheck()),
		"empty-triggers":          declarationImplementation(suspicious.EmptyTriggersCheck()),
		"empty-job":               declarationImplementation(correctness.EmptyJobCheck()),
		"empty-run":               declarationImplementation(suspicious.EmptyRunCheck()),
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
		"cache-write-untrusted": {Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			return declarationchecks.New(c.Rules, security.CacheWriteUntrustedCheck(workflow))
		}, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-write-untrusted") }},
		"cache-call-unrestricted": {Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			return declarationchecks.New(c.Rules, security.CacheCallUnrestrictedCheck(workflow))
		}, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") }},
		"cache-operation": {Prepare: prepareWorkflowMetadata, Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			return declarationchecks.New(c.Rules, correctness.CacheOperationCheck(workflow, c.Workflows))
		}, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-operation") }},
	}
})

func declarationImplementation(check checkinputs.DeclarationCheck) implementation {
	return implementation{Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, check) }}
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
		out[i] = Registration{Rule: metadata, Prepare: behavior.Prepare, Build: behavior.Build, Script: behavior.Script, Enabled: behavior.Enabled}
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

func prepareWorkflowRunNames(c Inputs) (InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, "workflow-run-names") == "off" {
		return PreparedInputs{Inputs: c}, nil
	}
	workflow, available := c.SourceSnapshot.WorkflowView()
	if !available || len(namecheck.WorkflowRunFilters(workflow)) == 0 {
		return PreparedInputs{Inputs: c}, nil
	}
	inventory, err := c.WorkflowNames.ForRoot(absolutePath(c.ProjectRoot))
	if err != nil {
		return nil, err
	}
	if !inventory.Complete() {
		return UnavailableInputs{Reason: "not every repository workflow name could be determined"}, nil
	}
	c.WorkflowNameInventory = inventory
	return PreparedInputs{Inputs: c}, nil
}

func prepareWorkflowMetadata(c Inputs) (InputPreparation, error) {
	if c.WorkflowSources != nil {
		if err := c.WorkflowSources.Prepare(c.Context, c.SourceSnapshot); err != nil {
			return nil, err
		}
		c.Workflows = c.WorkflowSources.Resolved()
	}
	return PreparedInputs{Inputs: c}, nil
}

func prepareExecutableIndex(c Inputs) (InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, "executable-bit") == "off" {
		return PreparedInputs{Inputs: c}, nil
	}
	if err := c.Context.Err(); err != nil {
		return nil, err
	}
	snapshot := c.Index(c.Context, c.ProjectRoot)
	if err := c.Context.Err(); err != nil {
		return nil, err
	}
	if err := snapshot.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		return UnavailableInputs{Reason: "Git index could not be read: " + err.Error()}, nil
	}
	c.Reader.Record(snapshot.Path())
	c.ExecutableIndex = snapshot
	return PreparedInputs{Inputs: c}, nil
}

func preparePermissionRequirements(c Inputs) (InputPreparation, error) {
	locations, err := c.Config.PermissionDeclarationLocations()
	if err != nil {
		return nil, err
	}
	c.RequiredPermissionLocations = locations
	return PreparedInputs{Inputs: c}, nil
}
