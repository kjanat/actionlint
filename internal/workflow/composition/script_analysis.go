package composition

import (
	"errors"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func scriptDefaultShell(defaults workflowdecl.DefaultsView) *string {
	shell, known := execution.DefaultShell(defaults.Run().Semantic()).ScalarString()
	if !known {
		return nil
	}
	return new(shell)
}

func (analysis *compositeAnalyzer) collectScript(declaration workflowdecl.StepView, cfg *repoconfig.Config, jobShell, workflowShell *string) error {
	if err := analysis.sourceSnapshot.ValidateStep(declaration); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
		return errors.New("script source snapshot is unavailable")
	}
	if action, ok := declaration.Action(); ok {
		return analysis.collectActionInputs(declaration, action, cfg)
	}
	run, ok := declaration.Run()
	if !ok || !run.Run().Present() {
		return nil
	}
	// Rejected run values have recovery strings without a source scalar.
	if !run.Run().SourceScalar().HasSource {
		return nil
	}
	occurrence, err := analysis.invocations.AddScript(analysis.context, declaration.Reference(), invocation.ScriptRun, "")
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
			jobShell, workflowShell, runnerShell = value.Value{}, value.Value{}, value.Value{}
			jobDir, workflowDir = execution.Directory{}, execution.Directory{}
		}
		request.Execution = scriptexecution.Execution{
			Shell:     execution.ResolveShell(run.Shell().Semantic(), jobShell, workflowShell, runnerShell),
			Directory: paths.Resolve(paths.EffectiveDirectory(run.WorkingDirectory().Semantic(), jobDir, workflowDir), analysis.reader),
		}
	}
	analysis.scripts = append(analysis.scripts, request)
	return nil
}

func (analysis *compositeAnalyzer) collectActionInputs(step workflowdecl.StepView, action workflowdecl.ExecActionView, cfg *repoconfig.Config) error {
	name, valid := scriptexecution.ScriptInputAction(action)
	if !valid {
		return nil
	}
	inputValues, _ := action.InputNames()
	inputs := maps.Collect(inputValues)
	var catalogue []analyzer.ScriptInput
	for _, inputName := range slices.Sorted(maps.Keys(inputs)) {
		input := inputs[inputName]
		if !input.Value().Present() || !input.Value().SourceScalar().HasSource || !input.Source().Present() {
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
		if !analysis.sourceSnapshot.Present() {
			return errors.New("action-input source snapshot is unavailable")
		}
		occurrence, err := analysis.invocations.AddScript(analysis.context, step.Reference(), invocation.ScriptActionInput, site.Input)
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
