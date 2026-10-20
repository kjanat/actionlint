// Package facts projects workflow declarations into source identities and analyzer inputs.
package facts

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/sourceid"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/source"
)

type References struct {
	Document  source.Reference
	Steps     map[*workflowlang.Step]source.Reference
	Jobs      map[string]source.Reference
	Positions map[[2]int]source.Reference
}

func Index(path string, content []byte, workflow *workflowlang.Workflow, action bool) References {
	index := sourceid.New(path, content)
	refs := References{Document: index.Ref("", "document", "", "$"), Steps: map[*workflowlang.Step]source.Reference{}, Jobs: map[string]source.Reference{}, Positions: map[[2]int]source.Reference{}}
	if workflow == nil {
		return refs
	}
	var walk func(string, string, []*workflowlang.Step)
	walk = func(owner, address string, steps []*workflowlang.Step) {
		for i, step := range steps {
			place := fmt.Sprintf("%s[%d]", address, i)
			key := stringValue(step.ID)
			ref := index.Ref(owner, key, stepFingerprint(step), place)
			refs.Steps[step] = ref
			if step.Pos != nil {
				refs.Positions[[2]int{step.Pos.Line, step.Pos.Col}] = ref
			}
			if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
				walk(ref.Node, place+".parallel", parallel.Steps)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		jobAddress := "jobs[" + strconv.Quote(id) + "]"
		refs.Jobs[id] = index.Ref("", "job:"+id, "", jobAddress)
		job := workflow.Jobs[id]
		for _, pos := range []*workflowlang.Pos{job.Pos, stringPos(job.ID)} {
			if pos != nil {
				refs.Positions[[2]int{pos.Line, pos.Col}] = refs.Jobs[id]
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

func stringValue(value *workflowlang.String) string {
	if value == nil {
		return ""
	}
	return value.Value
}
