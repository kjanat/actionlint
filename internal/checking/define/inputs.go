package define

import (
	"context"
	"io"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/files/names"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

// RuleInputs supplies captured sources, resolved settings and model facts to rule constructors.
// It contains no process pool, application session or engine implementation.
// Context controls cancellation for rule work, including filesystem reads.
type RuleInputs struct {
	Bindings                    *rulebinding.Scope
	RequiredPermissionLocations []configuration.PermissionLocation
	Lifecycle                   actionmeta.LifecycleContext

	Debug                         io.Writer
	Rules                         configuration.RuleSet
	SourceSnapshot                parsing.SourceSnapshot
	Runners                       *runners.Knowledge
	Context                       context.Context
	Path, WorkingDir, ProjectRoot string
	Config                        *configuration.Config
	Repository                    repository.Snapshot
	Actions                       *workspace.Resolver
	RemoteActions                 *remote.ResolvedActions
	WorkflowSources               *workflows.Cache
	Workflows                     *workflows.ResolvedWorkflows
	WorkflowNames                 *names.Index
	WorkflowNameInventory         namemodel.Inventory
	Reader                        *files.Reader
	Index                         func(context.Context, string) *execution.IndexSnapshot
	ExecutableIndex               *execution.IndexSnapshot
}

// DocumentPath returns the absolute path of the checked document.
func (c RuleInputs) DocumentPath() string {
	if filepath.IsAbs(c.Path) {
		return c.Path
	}
	return filepath.Join(c.WorkingDir, c.Path)
}

// InProjectWorkflows reports whether the checked document is a workflow
// directly under the project's .github/workflows directory and the session
// can inventory that directory's workflow names.
func (c RuleInputs) InProjectWorkflows() bool {
	if c.ProjectRoot == "" || c.WorkflowNames == nil {
		return false
	}
	return names.SamePath(filepath.Dir(c.DocumentPath()), filepath.Join(c.projectRoot(), ".github", "workflows"))
}

// ProjectWorkflowNames returns the inventory of the project's workflow names.
// Call it only when [RuleInputs.InProjectWorkflows] reports true.
func (c RuleInputs) ProjectWorkflowNames() (namemodel.Inventory, error) {
	return c.WorkflowNames.ForRoot(c.projectRoot())
}

func (c RuleInputs) projectRoot() string {
	if absolute, err := filepath.Abs(c.ProjectRoot); err == nil {
		return absolute
	}
	return c.ProjectRoot
}

// PrepareWorkflowMetadata resolves the reusable-workflow metadata that
// workflow-call checks consult, when the session supplies a workflow cache.
func PrepareWorkflowMetadata(c RuleInputs) (InputPreparation, error) {
	if c.WorkflowSources != nil {
		if err := c.WorkflowSources.Prepare(c.Context, c.SourceSnapshot); err != nil {
			return nil, err
		}
		c.Workflows = c.WorkflowSources.Resolved()
	}
	return PreparedInputs{RuleInputs: c}, nil
}
