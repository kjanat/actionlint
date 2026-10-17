package engine

import (
	"context"

	analyzerhost "actionlint.kjanat.dev/internal/analyzerhost"
	repoconfig "actionlint.kjanat.dev/internal/config"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/builtin"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
)

type ruleContext struct {
	workflowNames           *workflownames.Index
	path                    string
	config                  *repoconfig.Config
	projectConfig           *repoconfig.Config
	actions                 *actionlocal.Resolver
	workflows               *calls.Cache
	process                 *analyzerhost.ProcessPool
	workingDir, projectRoot string
	reader                  *fsread.Reader
	gitModes                *gitModes
}

func (c ruleContext) builtinContext() builtin.Context {
	ctx := context.Background()
	if c.process != nil {
		ctx = c.process.Ctx
	}
	var index func(context.Context, string) *execution.IndexSnapshot
	if c.gitModes != nil {
		index = c.gitModes.load
	}
	return builtin.Context{Context: ctx, Path: c.path, Config: c.config, Actions: c.actions, Workflows: c.workflows,
		WorkingDir: c.workingDir, ProjectRoot: c.projectRoot, Reader: c.reader, WorkflowNames: c.workflowNames, Index: index}
}
