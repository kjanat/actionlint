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
	StepJobs  map[*workflowlang.Step]string
	Jobs      map[string]source.Reference
	Positions map[[2]int]source.Reference
}

func Index(path string, content []byte, workflow *workflowlang.Workflow, action bool) References {
	return BuildSkeleton(workflow, action).Bind(path, content)
}

// Skeleton records declaration identity before publication. Binding supplies
// only document identity and revision; it never rereads mutable syntax values.
type Skeleton struct {
	jobs []skeletonJob
}

type skeletonJob struct {
	id, address string
	positions   []workflowlang.Pos
	steps       []skeletonStep
}

type skeletonStep struct {
	step                      *workflowlang.Step
	key, fingerprint, address string
	job                       string
	position                  workflowlang.Pos
	hasPosition               bool
	children                  []skeletonStep
}

func BuildSkeleton(workflow *workflowlang.Workflow, action bool) Skeleton {
	var skeleton Skeleton
	if workflow == nil {
		return skeleton
	}
	var walk func(string, string, []*workflowlang.Step) []skeletonStep
	walk = func(address, job string, steps []*workflowlang.Step) []skeletonStep {
		result := make([]skeletonStep, 0, len(steps))
		for i, step := range steps {
			place := fmt.Sprintf("%s[%d]", address, i)
			node := skeletonStep{step: step, key: stringValue(step.ID), fingerprint: stepFingerprint(step), address: place, job: job}
			if step.Pos != nil {
				node.position, node.hasPosition = *step.Pos, true
			}
			if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
				node.children = walk(place+".parallel", job, parallel.Steps)
			}
			result = append(result, node)
		}
		return result
	}
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[id]
		entry := skeletonJob{id: id, address: "jobs[" + strconv.Quote(id) + "]"}
		for _, pos := range []*workflowlang.Pos{job.Pos, stringPos(job.ID)} {
			if pos != nil {
				entry.positions = append(entry.positions, *pos)
			}
		}
		address := entry.address + ".steps"
		if action {
			address = "runs.steps"
		}
		entry.steps = walk(address, stringValue(job.ID), job.Steps)
		skeleton.jobs = append(skeleton.jobs, entry)
	}
	return skeleton
}

func (s Skeleton) Bind(path string, content []byte) References {
	index := sourceid.New(path, content)
	refs := References{Document: index.Ref("", "document", "", "$"), Steps: map[*workflowlang.Step]source.Reference{}, StepJobs: map[*workflowlang.Step]string{}, Jobs: map[string]source.Reference{}, Positions: map[[2]int]source.Reference{}}
	var walk func(string, []skeletonStep)
	walk = func(owner string, steps []skeletonStep) {
		for _, step := range steps {
			ref := index.Ref(owner, step.key, step.fingerprint, step.address)
			refs.Steps[step.step] = ref
			refs.StepJobs[step.step] = step.job
			if step.hasPosition {
				refs.Positions[[2]int{step.position.Line, step.position.Col}] = ref
			}
			walk(ref.Node, step.children)
		}
	}
	for _, job := range s.jobs {
		refs.Jobs[job.id] = index.Ref("", "job:"+job.id, "", job.address)
		for _, pos := range job.positions {
			refs.Positions[[2]int{pos.Line, pos.Col}] = refs.Jobs[job.id]
		}
		walk("job:"+job.id, job.steps)
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
