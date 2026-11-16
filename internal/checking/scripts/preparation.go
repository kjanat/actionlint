package scripts

import (
	"fmt"
	"slices"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ScriptInputAction removes the ref and lowercases the owner and repository.
// It preserves the case of any action subpath.
func ScriptInputAction(action *workflowlang.ExecAction) (string, bool) {
	if action == nil || action.Uses == nil || action.Uses.ContainsExpression() {
		return "", false
	}
	name, _, pinned := strings.Cut(action.Uses.Text(), "@")
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
	declarations := make(map[source.Reference]workflowdecl.Step)
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
			Scope: scope, Execution: request.Execution,
			Environment:  slices.Clone(request.Environment),
			Interpreters: request.Interpreters,
			Origin:       origin, Owner: &findingmodel.Owner{Job: job, Step: source.NilUnlessPresent(decl.Position())},
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

func scriptJob(graph invocation.Graph, declarations *parsing.SourceSnapshots, owner invocation.ID, declaredJob string) (string, error) {
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

func bindScript(unit *Unit, record invocation.Script, request ScriptRequest, declaration workflowdecl.Step) error {
	switch record.Kind {
	case invocation.ScriptRun:
		run, hasRun := declaration.Run()
		if request.Input != nil || !hasRun || run.Source == nil || run.RunPos == nil {
			return fmt.Errorf("%w: run occurrence %q needs one mapped run body", ErrInvalidBatch, record.ID)
		}
		if _, hasBodyPos := run.Run.Position(); !hasBodyPos {
			return fmt.Errorf("%w: run occurrence %q needs one mapped run body", ErrInvalidBatch, record.ID)
		}
		unit.Run, unit.Shell = run, request.DefaultShell
		if run.Shell != nil {
			unit.Shell = new(run.Shell.Value)
		}
	case invocation.ScriptActionInput:
		site := request.Input
		action, hasAction := declaration.Action()
		var input *workflowlang.ActionArgument
		inputs, _ := workflowdecl.InputNames(action)
		for name, candidate := range inputs {
			if name == record.Input {
				input = candidate
				break
			}
		}
		if site == nil || site.Input != record.Input || request.DefaultShell != nil || input == nil || input.Source == nil || !hasPosition(input.Name) || !hasPosition(input.Value) {
			return fmt.Errorf("%w: input occurrence %q needs its registered mapped argument", ErrInvalidBatch, record.ID)
		}
		if err := checkinputs.ValidateScriptInput(*site); err != nil {
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

func hasPosition(s *workflowlang.String) bool {
	_, present := s.Position()
	return present
}
