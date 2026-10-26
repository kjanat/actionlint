package composition

import (
	"errors"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func scriptDefaultShell(defaults *workflowlang.Defaults) *string {
	if defaults == nil || defaults.Run == nil || defaults.Run.Shell == nil {
		return nil
	}
	return &defaults.Run.Shell.Value
}

func (analysis *compositeAnalyzer) collectScript(step *workflowlang.Step, cfg *repoconfig.Config, jobShell, workflowShell *string) error {
	if action, ok := step.Exec.(*workflowlang.ExecAction); ok {
		return analysis.collectActionInputs(step, action, cfg)
	}
	run, ok := step.Exec.(*workflowlang.ExecRun)
	if !ok || run.Run == nil {
		return nil
	}
	if analysis.sourceSnapshot == nil {
		return errors.New("script source snapshot is unavailable")
	}
	reference, err := analysis.sourceSnapshot.StepReference(step)
	if err != nil {
		return err
	}
	occurrence, err := analysis.invocations.AddScript(analysis.context, reference, invocation.ScriptRun, "")
	if err != nil {
		return err
	}
	if !analysis.collectScripts {
		return nil
	}
	shell := jobShell
	if shell == nil {
		shell = workflowShell
	}
	request := scriptexecution.ScriptRequest{Occurrence: occurrence, DefaultShell: shell}
	if analysis.scriptScope != nil && analysis.resolveScriptExecution {
		paths := analysis.scriptScope.Paths
		if analysis.actions != nil {
			paths = analysis.actions.ApplyCheckout(paths)
			paths.Platform = analysis.actions.Platform()
		}
		jobShell, workflowShell, runnerShell := analysis.scriptJobShell, analysis.workflowShell, analysis.scriptRunnerShell
		jobDir, workflowDir := analysis.scriptJobDir, analysis.scriptWorkflowDir
		if analysis.scriptScope.Composite {
			jobShell, workflowShell, runnerShell = execution.ShellValue{}, execution.ShellValue{}, execution.ShellValue{}
			jobDir, workflowDir = execution.Directory{}, execution.Directory{}
		}
		request.Execution = scriptexecution.Execution{
			Shell:     execution.ResolveShell(run, jobShell, workflowShell, runnerShell),
			Directory: paths.Resolve(paths.EffectiveDirectory(run, jobDir, workflowDir)),
		}
	}
	analysis.scripts = append(analysis.scripts, request)
	return nil
}

func (analysis *compositeAnalyzer) collectActionInputs(step *workflowlang.Step, action *workflowlang.ExecAction, cfg *repoconfig.Config) error {
	name, valid := scriptexecution.ScriptInputAction(action)
	if !valid {
		return nil
	}
	inputs, _ := workflowlang.ActionInputNames(action)
	var catalogue []analyzer.ScriptInput
	for _, inputName := range slices.Sorted(maps.Keys(inputs)) {
		input := inputs[inputName]
		if input.Value == nil || input.Source == nil {
			continue // Expression-created values have no physical script scalar.
		}
		if catalogue == nil {
			var err error
			catalogue, err = analysis.actionInputCatalogue(cfg)
			if err != nil {
				return err
			}
		}
		var site analyzer.ScriptInput
		for _, candidate := range catalogue {
			if candidate.Action == name && candidate.Input == inputName {
				site = candidate
				break
			}
		}
		if site.Language == "" {
			continue
		}
		if analysis.sourceSnapshot == nil {
			return errors.New("action-input source snapshot is unavailable")
		}
		reference, err := analysis.sourceSnapshot.StepReference(step)
		if err != nil {
			return err
		}
		occurrence, err := analysis.invocations.AddScript(analysis.context, reference, invocation.ScriptActionInput, site.Input)
		if err != nil {
			return err
		}
		if !analysis.collectScripts {
			continue
		}
		analysis.scripts = append(analysis.scripts, scriptexecution.ScriptRequest{Occurrence: occurrence, Input: &site})
	}
	return nil
}

func (analysis *compositeAnalyzer) actionInputCatalogue(cfg *repoconfig.Config) ([]analyzer.ScriptInput, error) {
	if analysis.scriptInputsForConfig == nil {
		return analysis.scriptInputs, nil
	}
	if catalogue, exists := analysis.scriptCatalogues[cfg]; exists {
		return catalogue, nil
	}
	additional, err := analysis.scriptInputsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	catalogue, err := scriptInputCatalogue(append(slices.Clone(analysis.scriptInputs), additional...))
	if err != nil {
		return nil, err
	}
	analysis.scriptCatalogues[cfg] = catalogue
	return catalogue, nil
}
