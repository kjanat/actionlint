package engine

import (
	"errors"

	"actionlint.kjanat.dev/internal/fsread"
)

const commandGoodWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`

const commandBadWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ missing.value }}'
`

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// check lets cache-order regressions share one cache across explicitly ordered analyses.
func (l *Linter) check(path string, content []byte, project *Project, proc *concurrentProcess, actions *LocalActionsCache, workflows *LocalReusableWorkflowCache) ([]*Error, error) {
	engine := analysisEngine{
		analysisLogger: l.analysisLogger, ctx: l.ctx, reader: fsread.New(l.ctx, l.request.ReadFile, nil),
		buildRules: builtinFactory(l.request, l.analysisLogger),
		ignorePats: l.request.IgnorePatterns, onRulesCreated: l.request.OnRulesCreated,
	}
	if proc == nil {
		proc = newConcurrentProcess(l.ctx, 1)
	}
	var rules []RuleInfo
	errs, _, err := engine.check(path, content, project, l.source(path, content, project).Config, proc, actions, workflows, &rules)
	return errs, err
}
