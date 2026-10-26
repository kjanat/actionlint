package execution

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

// ScriptInputAction normalizes repository ownership while preserving subpath case.
func ScriptInputAction(action *workflowlang.ExecAction) (string, bool) {
	if action.Uses == nil || action.Uses.ContainsExpression() {
		return "", false
	}
	name, _, pinned := strings.Cut(action.Uses.Value, "@")
	parts := strings.SplitN(name, "/", 3)
	if !pinned || len(parts) < 2 {
		return "", false
	}
	parts[0], parts[1] = strings.ToLower(parts[0]), strings.ToLower(parts[1])
	return strings.Join(parts, "/"), true
}

type preparedDeclaration struct {
	source documentparse.StepDeclaration
	run    *workflowlang.ExecRun
	inputs map[string]*workflowlang.Input
}

func prepareScripts(graph invocation.Graph, collection Collection, scopes map[invocation.ID]*Scope) ([]Unit, error) {
	requests := make(map[invocation.ID]ScriptRequest, len(collection.Scripts))
	for _, request := range collection.Scripts {
		_, registered := graph.Script(request.Occurrence)
		_, duplicate := requests[request.Occurrence]
		if !registered || duplicate {
			return nil, fmt.Errorf("%w: unregistered or duplicated script %q", ErrInvalidBatch, request.Occurrence)
		}
		requests[request.Occurrence] = request
	}
	if !collection.Collected || len(requests) == 0 {
		return nil, nil
	}
	if _, exists := collection.Declarations.Lookup(graph.Entry().Reference); !exists {
		return nil, fmt.Errorf("%w: entry source is absent from declaration registry", ErrInvalidBatch)
	}
	declarations := make(map[source.Reference]preparedDeclaration)
	contents := make(map[source.Reference][]byte)
	units := make([]Unit, 0, len(requests))
	for _, record := range graph.Scripts() {
		scope := scopes[record.Owner]
		if scope == nil {
			return nil, fmt.Errorf("%w: script %q has no invocation scope", ErrInvalidBatch, record.ID)
		}
		decl, exists := declarations[record.Declaration]
		if !exists {
			original, found := collection.Declarations.Step(record.Declaration)
			if !found || original.Step == nil {
				return nil, fmt.Errorf("%w: script %q has no indexed declaration", ErrInvalidBatch, record.ID)
			}
			decl.source = original
			switch body := original.Step.Exec.(type) {
			case *workflowlang.ExecRun:
				decl.run = copyRun(body)
			case *workflowlang.ExecAction:
				inputs, _ := workflowlang.ActionInputNames(body)
				decl.inputs = make(map[string]*workflowlang.Input, len(inputs))
				for name, input := range inputs {
					decl.inputs[name] = copyInput(input)
				}
			}
			declarations[record.Declaration] = decl
		}
		job, err := scriptJob(graph, collection.Declarations, record.Owner, decl.source.Job)
		if err != nil {
			return nil, err
		}
		request := requests[record.ID]
		origin := findingmodel.StepOrigin(job, decl.source.Step)
		origin.StepPosition = copyValue(origin.StepPosition)
		unit := Unit{
			Occurrence: record.ID, Reference: record.Declaration,
			Config: scope.Config, Scope: scope, Execution: request.Execution,
			Origin: origin, Owner: &findingmodel.Owner{Job: job, Step: copyValue(decl.source.Step.Pos)},
			Path: scope.Deployment.SourcePath(),
		}
		if unit.Path != "" {
			ref := decl.source.Source.Reference()
			content, found := contents[ref]
			if !found {
				content = decl.source.Source.Content()
				contents[ref] = content
			}
			unit.Content = content
		}
		if err := bindScript(&unit, record, request, decl); err != nil {
			return nil, err
		}
		units = append(units, unit)
	}
	return units, nil
}

func scriptJob(graph invocation.Graph, declarations *documentparse.SourceSnapshots, owner invocation.ID, declaredJob string) (string, error) {
	if owner == graph.RootID() {
		return declaredJob, nil
	}
	path, exists := graph.Path(owner)
	if !exists || len(path) == 0 {
		return "", fmt.Errorf("%w: script owner has no invocation path", ErrInvalidBatch)
	}
	outer, exists := declarations.Step(path[0].Site())
	if !exists {
		return "", fmt.Errorf("%w: outer call has no indexed declaration", ErrInvalidBatch)
	}
	return outer.Job, nil
}

func bindScript(unit *Unit, record invocation.Script, request ScriptRequest, declaration preparedDeclaration) error {
	switch record.Kind {
	case invocation.ScriptRun:
		run := declaration.run
		if request.Input != nil || run == nil || run.Run == nil || run.Source == nil || run.Run.Pos == nil || run.RunPos == nil {
			return fmt.Errorf("%w: run occurrence %q needs one mapped run body", ErrInvalidBatch, record.ID)
		}
		unit.Run, unit.Shell = run, copyValue(request.DefaultShell)
		if run.Shell != nil {
			unit.Shell = copyValue(&run.Shell.Value)
		}
	case invocation.ScriptActionInput:
		site := request.Input
		input := declaration.inputs[record.Input]
		if site == nil || site.Input != record.Input || request.DefaultShell != nil || input == nil || input.Name == nil || input.Name.Pos == nil || input.Value == nil || input.Value.Pos == nil || input.Source == nil {
			return fmt.Errorf("%w: input occurrence %q needs its registered mapped argument", ErrInvalidBatch, record.ID)
		}
		if err := analyzer.ValidateScriptInput(*site); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidBatch, err)
		}
		action, ok := declaration.source.Step.Exec.(*workflowlang.ExecAction)
		if !ok {
			return fmt.Errorf("%w: input declaration is not an action", ErrInvalidBatch)
		}
		name, valid := ScriptInputAction(action)
		if !valid || name != site.Action {
			return fmt.Errorf("%w: input selector disagrees with declared action", ErrInvalidBatch)
		}
		unit.Input = &ActionInput{Site: *site, Value: input}
	default:
		return fmt.Errorf("%w: unknown script kind", ErrInvalidBatch)
	}
	return nil
}
