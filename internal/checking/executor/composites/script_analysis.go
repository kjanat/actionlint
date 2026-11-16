package composites

import (
	"errors"
	"maps"
	"slices"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	"actionlint.kjanat.dev/v2/internal/input/files/runnerpaths"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func scriptDefaultShell(defaults *workflowlang.Defaults) *string {
	shell, known := execution.DefaultShell(workflowdecl.DefaultsRun(defaults)).ScalarString()
	if !known {
		return nil
	}
	return new(shell)
}

func (analysis *compositeAnalyzer) collectScript(declaration workflowdecl.Step, jobShell, workflowShell *string) error {
	if err := analysis.sourceSnapshot.ValidateStep(declaration); err != nil {
		return err
	}
	if action, ok := declaration.Action(); ok {
		return analysis.collectActionInputs(declaration, action)
	}
	run, ok := declaration.Run()
	// Rejected run values have recovery strings without a source scalar.
	if !ok || run.Run == nil || !run.Run.SourceScalar.HasSource {
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
	request := scriptexecution.ScriptRequest{Occurrence: occurrence, DefaultShell: shell, Interpreters: commands.FromContext(analysis.ctx)}
	jobDefault, workflowDefault, runnerDefault := analysis.scriptJobShell, analysis.workflowShell, analysis.scriptRunnerShell
	if analysis.scriptScope != nil && analysis.scriptScope.Composite {
		jobDefault, workflowDefault, runnerDefault = value.Value{}, value.Value{}, value.Value{}
	}
	resolvedShell := execution.ResolveShell(workflowdecl.Scalar(run.Shell), jobDefault, workflowDefault, runnerDefault)
	request.Environment = analysis.scriptEnvironment(declaration, resolvedShell.Name)
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
			Shell:     execution.ResolveShell(workflowdecl.Scalar(run.Shell), jobShell, workflowShell, runnerShell),
			Directory: runnerpaths.ResolveDirectory(paths, paths.EffectiveDirectory(workflowdecl.Scalar(run.WorkingDirectory), jobDir, workflowDir), analysis.reader),
		}
	}
	analysis.scripts = append(analysis.scripts, request)
	return nil
}

func (analysis *compositeAnalyzer) collectActionInputs(step workflowdecl.Step, action *workflowlang.ExecAction) error {
	name, valid := scriptexecution.ScriptInputAction(action)
	if !valid {
		return nil
	}
	inputValues, _ := workflowdecl.InputNames(action)
	inputs := maps.Collect(inputValues)
	var catalogue []checkinputs.ScriptInput
	for _, inputName := range slices.Sorted(maps.Keys(inputs)) {
		input := inputs[inputName]
		if input == nil || input.Value == nil || !input.Value.SourceScalar.HasSource || input.Source == nil {
			continue // Expression-created values have no physical script scalar.
		}
		if catalogue == nil {
			var err error
			catalogue, err = analysis.actionInputCatalogue()
			if err != nil {
				return err
			}
		}
		var site checkinputs.ScriptInput
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
		analysis.scripts = append(analysis.scripts, scriptexecution.ScriptRequest{Occurrence: occurrence, Input: &site, Environment: analysis.scriptEnvironment(step, site.Language)})
	}
	return nil
}

func (analysis *compositeAnalyzer) scriptEnvironment(step workflowdecl.Step, shell string) []scriptmodel.EnvironmentNames {
	// ProcessInvoker overlays step variables on the inherited process environment.
	// https://github.com/actions/runner/blob/main/src/Runner.Sdk/ProcessInvoker.cs
	layers := []scriptmodel.EnvironmentLayer{{Scope: scriptmodel.EnvironmentRuntime, UnknownNames: true}}
	layers = append(layers, analysis.environmentLayers...)
	layers = append(layers, scriptmodel.DeclaredEnvironment(scriptmodel.EnvironmentStep, workflowdecl.Environment(step.Env)))
	if target := analysis.runners.Target(); target == "" || target == "github" {
		composite := analysis.scriptScope != nil && analysis.scriptScope.Composite
		// ScriptHandler exports runtime contexts after the step environment.
		layers = append(layers, scriptmodel.EnvironmentLayer{Scope: scriptmodel.EnvironmentRuntime, Names: runnerlang.RuntimeEnvironmentNames(composite)})
	}
	return scriptmodel.EnvironmentAlternatives(shell, analysis.operatingSystems, layers...)
}

func (analysis *compositeAnalyzer) actionInputCatalogue() ([]checkinputs.ScriptInput, error) {
	if analysis.scriptInputsForScope == nil {
		return analysis.scriptInputs, nil
	}
	if catalogue, exists := analysis.scriptCatalogues[analysis.bindings]; exists {
		return catalogue, nil
	}
	additional, err := analysis.scriptInputsForScope(analysis.bindings)
	if err != nil {
		return nil, err
	}
	catalogue, err := scriptInputCatalogue(append(slices.Clone(analysis.scriptInputs), additional...))
	if err != nil {
		return nil, err
	}
	analysis.scriptCatalogues[analysis.bindings] = catalogue
	return catalogue, nil
}
