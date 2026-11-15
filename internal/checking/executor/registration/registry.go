package registration

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	"actionlint.kjanat.dev/v2/internal/model/commands"

	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	"actionlint.kjanat.dev/v2/internal/checking/expressions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	"actionlint.kjanat.dev/v2/internal/input/files"

	workflownames "actionlint.kjanat.dev/v2/internal/input/files/names"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/repository"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

// Inputs supplies captured sources, resolved settings and model facts to rule constructors.
// It contains no process pool, application session or engine implementation.
// Context controls cancellation for rule work, including filesystem reads.
type Inputs struct {
	Bindings                    *binding.Scope
	RequiredPermissionLocations []repoconfig.PermissionLocation
	Lifecycle                   actionmeta.LifecycleContext

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

var nativeRuleCatalog = sync.OnceValue(func() []ruleSpec {
	return []ruleSpec{
		newSpec(correctness.VendorLockInDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.NewVendorLockIn(c.Rules, c.Runners) }}),
		newSpec(correctness.ContainerRunnerDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.NewContainerRunner(c.Context, c.Runners, c.Actions, c.RemoteActions)
		}}),
		declarationSpec(suspicious.UncancellableConditionCheck()),
		newSpec(security.MissingPermissionsDeclaration(), implementation{Prepare: preparePermissionRequirements, Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, security.MissingPermissionsCheck(c.RequiredPermissionLocations))
		}}),
		newSpec(conventions.MissingActionMetadataDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, conventions.MissingActionMetadataCheck(c.RemoteActions))
		}, Enabled: func(c Inputs) bool {
			return c.Config != nil && c.Config.Policy.RequireActionMetadata != nil && *c.Config.Policy.RequireActionMetadata
		}}),
		declarationOnly(correctness.ExpressionSyntaxDeclaration()),
		declarationOnly(correctness.ExpressionReferenceDeclaration()),
		declarationOnly(correctness.ContextAvailabilityDeclaration()),
		declarationOnly(correctness.ExpressionTypeDeclaration()),
		declarationOnly(correctness.ExpressionCallDeclaration()),
		declarationOnly(correctness.EventPayloadDeclaration()),
		declarationOnly(security.ScriptInjectionDeclaration()),
		declarationOnly(correctness.ScriptInterpolationDeclaration()),
		newSpec(correctness.WorkflowRunNamesDeclaration(), implementation{Prepare: prepareWorkflowRunNames, Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.WorkflowRunNamesCheck(c.WorkflowNameInventory))
		}, Enabled: workflowRunNamesAvailable}),
		declarationOnly(correctness.UnsoundTernaryDeclaration()),
		declarationOnly(correctness.SyntaxCheckDeclaration()),
		declarationOnly(correctness.InlineSuppressionDeclaration()),
		declarationOnly(correctness.UnusedSuppressionDeclaration()),
		declarationOnly(correctness.MissingLocalActionDeclaration()),
		declarationOnly(suspicious.StringConditionsDeclaration()),
		declarationOnly(suspicious.JoinLiteralDeclaration()),
		declarationOnly(suspicious.CollectionComparisonDeclaration()),
		declarationOnly(suspicious.CoercionDeclaration()),
		declarationOnly(suspicious.MixedTypeComparisonsDeclaration()),
		declarationOnly(suspicious.CaseInsensitiveConditionsDeclaration()),
		declarationOnly(suspicious.MixedTypeMatrixFiltersDeclaration()),
		declarationOnly(conventions.ForbiddenSuppressionDeclaration()),
		newSpec(correctness.MatrixDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.MatrixCheck(c.Config.RuleSet().DiagnosticLevel(c.Config, "mixed-type-matrix-filters") != "off"))
		}}),
		declarationSpec(security.CredentialsCheck()),
		newSpec(correctness.ShellNameDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.ShellNameCheck(c.Runners, commands.FromContext(c.Context)))
		}}),
		newSpec(correctness.RunnerLabelDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			labels := runnerlang.LabelSet{}
			if known, available := repoconfig.RepositoryFacts(c.Config, c.Repository).RunnerLabels.Value(); available {
				labels = runnerlang.KnownLabels(known.Values())
			}
			return declarationchecks.New(c.Rules, correctness.RunnerLabelCheck(c.Runners, labels))
		}}),
		newSpec(correctness.RunnerSupportDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.RunnerSupportCheck(c.Runners))
		}}),
		declarationSpec(correctness.EventsCheck()),
		declarationSpec(correctness.JobNeedsCheck()),
		declarationSpec(correctness.ParallelStepsCheck()),
		newSpec(correctness.ActionDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleAction(c.Context, c.Actions.Prepared(), c.RemoteActions).WithLifecycle(c.Lifecycle)
		}}),
		newSpec(correctness.RuntimeLifecycleDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.New(c.Rules, correctness.RuntimeLifecycleCheck(c.Lifecycle))
		}}),
		newSpec(correctness.ExecutableBitDeclaration(), implementation{Prepare: prepareExecutableIndex, Build: func(c Inputs) workflowvisit.Rule {
			return correctness.NewRuleExecutableBit(correctness.ExecutableContext{
				ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
				Index: c.ExecutableIndex, Runners: c.Runners,
			})
		}, Enabled: func(c Inputs) bool { return c.ProjectRoot != "" && c.Index != nil }}),
		declarationSpec(correctness.EnvVarCheck()),
		declarationSpec(correctness.IdCheck()),
		declarationSpec(correctness.GlobCheck()),
		declarationSpec(correctness.PermissionsCheck()),
		newSpec(correctness.WorkflowCallDeclaration(), implementation{Prepare: prepareWorkflowMetadata, Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			logger := reporting.Logger{Tag: "workflow-call", Writer: c.Debug}
			return declarationchecks.New(c.Rules, correctness.WorkflowCallCheck(workflow, c.Workflows, repoconfig.RepositoryFacts(c.Config, c.Repository), logger.Debug))
		}}),
		newSpec(correctness.ExpressionDeclaration(), implementation{Prepare: prepareWorkflowMetadata, Build: func(c Inputs) workflowvisit.Rule {
			return expressions.NewExpressionRule(c.Context, c.Actions.Prepared(), c.Workflows, c.Runners, c.RemoteActions)
		}}),
		newSpec(correctness.DeprecatedCommandsDeclaration(), implementation{Script: correctness.DeprecatedCommandsChecker{}}),
		newSpec(suspicious.ErrexitStatusDeclaration(), implementation{Script: suspicious.ErrexitChecker()}),
		newSpec(suspicious.WorkflowCommandSyntaxDeclaration(), implementation{Script: suspicious.WorkflowCommandChecker()}),
		newSpec(correctness.InvalidOutputFileDeclaration(), implementation{Script: correctness.InvalidOutputFileChecker{}}),
		declarationSpec(correctness.IfCondCheck()),
		declarationSpec(correctness.NumericValuesCheck()),
		declarationSpec(suspicious.EmptyTriggersCheck()),
		declarationSpec(correctness.EmptyJobCheck()),
		declarationSpec(suspicious.EmptyRunCheck()),
		newSpec(security.UnpinnedReferenceDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
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
		}, Enabled: func(c Inputs) bool { return c.Config.RequiresCommitHash() }}),
		newSpec(conventions.JobTimeoutDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			return declarationchecks.NewRuleRequireJobTimeout(c.Rules, c.Config.RequiresJobTimeout())
		}, Enabled: func(c Inputs) bool { return c.Config.RequiresJobTimeout().Enabled() }}),
		newSpec(security.CacheWriteUntrustedDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			return declarationchecks.New(c.Rules, security.CacheWriteUntrustedCheck(workflow))
		}, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-write-untrusted") }}),
		newSpec(security.CacheCallUnrestrictedDeclaration(), implementation{Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			return declarationchecks.New(c.Rules, security.CacheCallUnrestrictedCheck(workflow))
		}, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") }}),
		newSpec(correctness.CacheOperationDeclaration(), implementation{Prepare: prepareWorkflowMetadata, Build: func(c Inputs) workflowvisit.Rule {
			workflow, _ := c.SourceSnapshot.WorkflowView()
			return declarationchecks.New(c.Rules, correctness.CacheOperationCheck(workflow, c.Workflows))
		}, Enabled: func(c Inputs) bool { return c.Config.CachePolicyEnabled("cache-operation") }}),
	}
})

func declarationImplementation(check checkinputs.DeclarationCheck) implementation {
	return implementation{Build: func(c Inputs) workflowvisit.Rule { return declarationchecks.New(c.Rules, check) }}
}

// Registrations returns check implementations in rule-declaration order.
func Registrations(rules repoconfig.RuleSet) []Registration {
	behaviors := make(map[string]implementation)
	for _, spec := range nativeRuleCatalog() {
		behavior := spec.behavior
		if behavior.Build == nil && behavior.Script == nil {
			continue
		}
		name := spec.declaration.Name
		if _, found := rules.FindRule(name); !found {
			panic("check implementation " + name + " has no rule declaration")
		}
		behaviors[name] = behavior
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
	for _, spec := range nativeRuleCatalog() {
		if spec.declaration.Name == name {
			return spec.behavior.Build != nil || spec.behavior.Script != nil
		}
	}
	return false
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
	if !available || len(correctness.WorkflowRunFilters(workflow)) == 0 {
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
