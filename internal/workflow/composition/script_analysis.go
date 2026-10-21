package composition

import (
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/analyzer"
	scriptexecution "actionlint.kjanat.dev/internal/tools/execution"

	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
)

func scriptDefaultShell(defaults *workflowlang.Defaults) *string {
	if defaults == nil || defaults.Run == nil || defaults.Run.Shell == nil {
		return nil
	}
	return &defaults.Run.Shell.Value
}

func (analysis *compositeAnalyzer) collectScript(step *workflowlang.Step, cfg *repoconfig.Config, path string, content []byte, jobShell, workflowShell *string) {
	if !analysis.collectScripts {
		return
	}
	if action, ok := step.Exec.(*workflowlang.ExecAction); ok {
		analysis.collectActionInputs(step, action, cfg, path, content)
		return
	}
	run, ok := step.Exec.(*workflowlang.ExecRun)
	if !ok || run.Run == nil {
		return
	}
	shell := jobShell
	if shell == nil {
		shell = workflowShell
	}
	if run.Shell != nil {
		shell = &run.Shell.Value
	}
	unit := scriptexecution.Unit{Run: run, Shell: shell, Config: cfg, Path: path, Content: content, Origin: findingmodel.StepOrigin(analysis.jobID, step), Owner: &findingmodel.Owner{Job: analysis.jobID, Step: step.Pos}, Scope: analysis.scriptScope}
	if unit.Scope != nil && analysis.resolveScriptExecution {
		paths := unit.Scope.Paths
		if analysis.actions != nil {
			paths = analysis.actions.ApplyCheckout(paths)
			paths.Platform = analysis.actions.Platform()
		}
		jobShell, workflowShell, runnerShell := analysis.scriptJobShell, analysis.workflowShell, analysis.scriptRunnerShell
		jobDir, workflowDir := analysis.scriptJobDir, analysis.scriptWorkflowDir
		if unit.Scope.Composite {
			jobShell, workflowShell, runnerShell = execution.ShellValue{}, execution.ShellValue{}, execution.ShellValue{}
			jobDir, workflowDir = execution.Directory{}, execution.Directory{}
		}
		unit.Execution = scriptexecution.Execution{
			Shell:     execution.ResolveShell(run, jobShell, workflowShell, runnerShell),
			Directory: paths.Resolve(paths.EffectiveDirectory(run, jobDir, workflowDir)),
		}
	}
	analysis.scripts = append(analysis.scripts, unit)
}

func (analysis *compositeAnalyzer) collectActionInputs(step *workflowlang.Step, action *workflowlang.ExecAction, cfg *repoconfig.Config, path string, content []byte) {
	if action.Uses == nil || action.Uses.ContainsExpression() {
		return
	}
	name, _, pinned := strings.Cut(action.Uses.Value, "@")
	if !pinned {
		return
	}
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 2 {
		return
	}
	parts[0], parts[1] = strings.ToLower(parts[0]), strings.ToLower(parts[1])
	name = strings.Join(parts, "/")
	inputs, _ := workflowlang.ActionInputNames(action)
	for _, inputName := range slices.Sorted(maps.Keys(inputs)) {
		input := inputs[inputName]
		if input.Value == nil || input.Source == nil {
			continue // Expression-created values have no physical script scalar.
		}
		var site analyzer.ScriptInput
		for _, candidate := range analysis.scriptInputs {
			if candidate.Action == name && candidate.Input == inputName {
				site = candidate
				break
			}
		}
		if site.Language == "" {
			continue
		}
		analysis.scripts = append(analysis.scripts, scriptexecution.Unit{
			Input:  &scriptexecution.ActionInput{Site: site, Value: input},
			Config: cfg, Path: path, Content: content, Origin: findingmodel.StepOrigin(analysis.jobID, step),
			Owner: &findingmodel.Owner{Job: analysis.jobID, Step: step.Pos}, Scope: analysis.scriptScope,
		})
	}
}
