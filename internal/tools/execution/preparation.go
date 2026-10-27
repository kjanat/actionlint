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
func ScriptInputAction(action documentparse.ExecActionView) (string, bool) {
	if !action.Uses().Present() || action.Uses().ContainsExpression() {
		return "", false
	}
	name, _, pinned := strings.Cut(action.Uses().Value(), "@")
	parts := strings.SplitN(name, "/", 3)
	if !pinned || len(parts) < 2 {
		return "", false
	}
	parts[0], parts[1] = strings.ToLower(parts[0]), strings.ToLower(parts[1])
	return strings.Join(parts, "/"), true
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
	declarations := make(map[source.Reference]documentparse.StepView)
	contents := make(map[source.Reference][]byte)
	units := make([]Unit, 0, len(requests))
	for _, record := range graph.Scripts() {
		scope := scopes[record.Owner]
		if scope == nil {
			return nil, fmt.Errorf("%w: script %q has no invocation scope", ErrInvalidBatch, record.ID)
		}
		decl, exists := declarations[record.Declaration]
		if !exists {
			original, found := collection.Declarations.Declaration(record.Declaration)
			if !found || !original.Present() {
				return nil, fmt.Errorf("%w: script %q has no indexed declaration", ErrInvalidBatch, record.ID)
			}
			decl = original
			declarations[record.Declaration] = decl
		}
		job, err := scriptJob(graph, collection.Declarations, record.Owner, decl.DeclaringJob())
		if err != nil {
			return nil, err
		}
		request := requests[record.ID]
		provenance := decl.Origin(job)
		origin := &findingmodel.IgnoreOrigin{Job: provenance.Job, StepID: provenance.StepID, StepName: provenance.StepName, Uses: provenance.Uses}
		if provenance.HasPosition {
			origin.StepPosition = new(provenance.Position)
		}
		unit := Unit{
			Occurrence: record.ID, Reference: record.Declaration,
			Config: scope.Config, Scope: scope, Execution: request.Execution,
			Origin: origin, Owner: &findingmodel.Owner{Job: job, Step: positionPointer(decl.Pos())},
			Path: scope.Deployment.SourcePath(),
		}
		captured, found := collection.Declarations.LookupDocument(record.Declaration.Document)
		if !found {
			return nil, fmt.Errorf("%w: script source capture is unavailable", ErrInvalidBatch)
		}
		unit.Revision, unit.Coordinates = captured.Capture().Revision(), captured.Capture().Coordinates()
		if unit.Path != "" {
			ref := decl.SourceReference()
			content, found := contents[ref]
			if !found {
				content = []byte(decl.ContentText())
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
	outer, exists := declarations.Declaration(path[0].Site())
	if !exists {
		return "", fmt.Errorf("%w: outer call has no indexed declaration", ErrInvalidBatch)
	}
	return outer.DeclaringJob(), nil
}

func bindScript(unit *Unit, record invocation.Script, request ScriptRequest, declaration documentparse.StepView) error {
	switch record.Kind {
	case invocation.ScriptRun:
		run, hasRun := declaration.Run()
		_, hasBodyPos := run.Run().Pos()
		_, hasRunPos := run.RunPos()
		if request.Input != nil || !hasRun || !run.Run().Present() || !run.Source().Present() || !hasBodyPos || !hasRunPos {
			return fmt.Errorf("%w: run occurrence %q needs one mapped run body", ErrInvalidBatch, record.ID)
		}
		unit.Run, unit.Shell = run, copyValue(request.DefaultShell)
		if run.Shell().Present() {
			unit.Shell = new(run.Shell().Value())
		}
	case invocation.ScriptActionInput:
		site := request.Input
		action, hasAction := declaration.Action()
		var input documentparse.InputView
		inputs, _ := action.InputNames()
		for name, candidate := range inputs {
			if name == record.Input {
				input = candidate
				break
			}
		}
		_, hasNamePos := input.Name().Pos()
		_, hasValuePos := input.Value().Pos()
		if site == nil || site.Input != record.Input || request.DefaultShell != nil || !input.Present() || !input.Name().Present() || !hasNamePos || !input.Value().Present() || !hasValuePos || !input.Source().Present() {
			return fmt.Errorf("%w: input occurrence %q needs its registered mapped argument", ErrInvalidBatch, record.ID)
		}
		if err := analyzer.ValidateScriptInput(*site); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidBatch, err)
		}
		if !hasAction {
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

// positionPointer detaches an optional value position for legacy finding adapters.
func positionPointer(position workflowlang.Pos, present bool) *workflowlang.Pos {
	if !present {
		return nil
	}
	return &position
}
