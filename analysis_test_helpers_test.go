package actionlint

import "errors"

const commandGoodWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
const commandBadWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// check lets cache-order regressions share one cache across explicitly ordered analyses.
func (l *Linter) check(path string, content []byte, project *Project, proc *concurrentProcess, actions *LocalActionsCache, workflows *LocalReusableWorkflowCache) ([]*Error, error) {
	engine := analysisEngine{
		analysisLogger: l.analysisLogger, ctx: l.ctx,
		shellcheck: l.request.ShellCheck, pyflakes: l.request.Pyflakes,
		shellcheckOptions: l.request.ShellcheckOptions, pyflakesOptions: l.request.PyflakesOptions,
		ignorePats: l.request.IgnorePatterns, onRulesCreated: l.request.OnRulesCreated,
	}
	var rules []Rule
	return engine.check(path, content, project, l.source(path, content, project).Config, proc, actions, workflows, &rules)
}
