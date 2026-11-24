package define

import (
	"context"
	"io"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/nameindex"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

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
	Repository                    repository.Settings
	Actions                       *localactions.Resolver
	RemoteActions                 *remote.ResolvedActions
	WorkflowSources               *reusableworkflows.MetadataCache
	Workflows                     *reusableworkflows.ResolvedWorkflows
	WorkflowNames                 *nameindex.Index
	WorkflowNameInventory         names.Inventory
	Reader                        *files.Reader
	Index                         func(context.Context, string) *execution.IndexSnapshot
	ExecutableIndex               *execution.IndexSnapshot
}

func (c RuleInputs) DocumentPath() string {
	if filepath.IsAbs(c.Path) {
		return c.Path
	}
	return filepath.Join(c.WorkingDir, c.Path)
}

func (c RuleInputs) InProjectWorkflows() bool {
	if c.ProjectRoot == "" || c.WorkflowNames == nil {
		return false
	}
	return nameindex.SamePath(filepath.Dir(c.DocumentPath()), filepath.Join(c.projectRoot(), ".github", "workflows"))
}

// ProjectWorkflowNames requires [RuleInputs.InProjectWorkflows] to report true.
func (c RuleInputs) ProjectWorkflowNames() (names.Inventory, error) {
	return c.WorkflowNames.ForRoot(c.projectRoot())
}

func (c RuleInputs) projectRoot() string {
	if absolute, err := filepath.Abs(c.ProjectRoot); err == nil {
		return absolute
	}
	return c.ProjectRoot
}

func PrepareWorkflowMetadata(c RuleInputs) (InputPreparation, error) {
	if c.WorkflowSources != nil {
		if err := c.WorkflowSources.Prepare(c.Context, c.SourceSnapshot); err != nil {
			return nil, err
		}
		c.Workflows = c.WorkflowSources.Resolved()
	}
	return PreparedInputs{RuleInputs: c}, nil
}
