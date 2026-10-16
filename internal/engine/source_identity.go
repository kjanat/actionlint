package engine

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"actionlint.kjanat.dev/internal/sourceid"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/source"
)

type sourceReferences struct {
	document  source.Reference
	steps     map[*workflowlang.Step]source.Reference
	jobs      map[string]source.Reference
	positions map[[2]int]source.Reference
}

func workflowReferences(path string, content []byte, workflow *workflowlang.Workflow, action bool) sourceReferences {
	index := sourceid.New(path, content)
	refs := sourceReferences{document: index.Ref("", "document", "", "$"), steps: map[*workflowlang.Step]source.Reference{}, jobs: map[string]source.Reference{}, positions: map[[2]int]source.Reference{}}
	if workflow == nil {
		return refs
	}
	var walk func(string, string, []*workflowlang.Step)
	walk = func(owner, address string, steps []*workflowlang.Step) {
		for i, step := range steps {
			place := fmt.Sprintf("%s[%d]", address, i)
			key := outlineString(step.ID)
			ref := index.Ref(owner, key, stepFingerprint(step), place)
			refs.steps[step] = ref
			if step.Pos != nil {
				refs.positions[[2]int{step.Pos.Line, step.Pos.Col}] = ref
			}
			if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
				walk(ref.Node, place+".parallel", parallel.Steps)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		jobAddress := "jobs[" + strconv.Quote(id) + "]"
		refs.jobs[id] = index.Ref("", "job:"+id, "", jobAddress)
		job := workflow.Jobs[id]
		for _, pos := range []*workflowlang.Pos{job.Pos, stringPos(job.ID)} {
			if pos != nil {
				refs.positions[[2]int{pos.Line, pos.Col}] = refs.jobs[id]
			}
		}
		address := jobAddress + ".steps"
		if action {
			address = "runs.steps"
		}
		walk("job:"+id, address, workflow.Jobs[id].Steps)
	}
	return refs
}

func stringPos(value *workflowlang.String) *workflowlang.Pos {
	if value == nil {
		return nil
	}
	return value.Pos
}
