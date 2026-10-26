package composition

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
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

func (analysis *compositeAnalyzer) collectScript(step *workflowlang.Step, cfg *repoconfig.Config, path string, content []byte, jobShell, workflowShell *string) error {
	if !analysis.collectScripts {
		return nil
	}
	if action, ok := step.Exec.(*workflowlang.ExecAction); ok {
		return analysis.collectActionInputs(step, action, cfg, path, content)
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
	occurrence, err := invocation.NewScriptID(analysis.occurrence, reference, invocation.ScriptRun, "")
	if err != nil {
		return err
	}
	shell := jobShell
	if shell == nil {
		shell = workflowShell
	}
	if run.Shell != nil {
		shell = &run.Shell.Value
	}
	unit := scriptexecution.Unit{Occurrence: occurrence, Reference: reference, Run: run, Shell: shell, Config: cfg, Path: path, Content: content, Origin: findingmodel.StepOrigin(analysis.jobID, step), Owner: &findingmodel.Owner{Job: analysis.jobID, Step: step.Pos}, Scope: analysis.scriptScope}
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
	return nil
}

func (analysis *compositeAnalyzer) collectActionInputs(step *workflowlang.Step, action *workflowlang.ExecAction, cfg *repoconfig.Config, path string, content []byte) error {
	if action.Uses == nil || action.Uses.ContainsExpression() {
		return nil
	}
	name, _, pinned := strings.Cut(action.Uses.Value, "@")
	if !pinned {
		return nil
	}
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 2 {
		return nil
	}
	parts[0], parts[1] = strings.ToLower(parts[0]), strings.ToLower(parts[1])
	name = strings.Join(parts, "/")
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
		occurrence, err := invocation.NewScriptID(analysis.occurrence, reference, invocation.ScriptActionInput, site.Input)
		if err != nil {
			return err
		}
		analysis.scripts = append(analysis.scripts, scriptexecution.Unit{
			Occurrence: occurrence,
			Reference:  reference,
			Input:      &scriptexecution.ActionInput{Site: site, Value: input},
			Config:     cfg, Path: path, Content: content, Origin: findingmodel.StepOrigin(analysis.jobID, step),
			Owner: &findingmodel.Owner{Job: analysis.jobID, Step: step.Pos}, Scope: analysis.scriptScope,
		})
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
