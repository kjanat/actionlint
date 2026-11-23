// Package facts projects workflow declarations into source identities and analyzer inputs.
package facts

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type References struct {
	Document  sourcetext.Reference
	Steps     map[*definition.Step]sourcetext.Reference
	StepJobs  map[*definition.Step]string
	Jobs      map[string]sourcetext.Reference
	Positions map[[2]int]sourcetext.Reference
}

type SourceIndex struct {
	Document  sourcetext.Reference
	Steps     []sourcetext.Reference
	Jobs      map[string]sourcetext.Reference
	Positions map[[2]int]sourcetext.Reference
}

func (r References) SourceIndex() SourceIndex {
	steps := make([]sourcetext.Reference, 0, len(r.Steps))
	for _, step := range r.Steps {
		steps = append(steps, step)
	}
	slices.SortFunc(steps, func(a, b sourcetext.Reference) int { return strings.Compare(a.Address, b.Address) })
	return SourceIndex{Document: r.Document, Steps: steps, Jobs: maps.Clone(r.Jobs), Positions: maps.Clone(r.Positions)}
}

// Index uses action-manifest step addresses when action is true.
func Index(path string, content []byte, workflow *definition.Workflow, action bool) References {
	return buildSkeleton(workflow, action).bind(path, content)
}

// skeleton never rereads mutable syntax values.
type skeleton struct {
	jobs []skeletonJob
}

type skeletonJob struct {
	id, address string
	positions   []sourcetext.Position
	steps       []skeletonStep
}

type skeletonStep struct {
	step                      *definition.Step
	key, fingerprint, address string
	job                       string
	position                  sourcetext.Position
	hasPosition               bool
	children                  []skeletonStep
}

func buildSkeleton(workflow *definition.Workflow, action bool) skeleton {
	var built skeleton
	if workflow == nil {
		return built
	}
	var walk func(string, string, []*definition.Step) []skeletonStep
	walk = func(address, job string, steps []*definition.Step) []skeletonStep {
		result := make([]skeletonStep, 0, len(steps))
		for i, step := range steps {
			place := fmt.Sprintf("%s[%d]", address, i)
			node := skeletonStep{step: step, key: stringValue(step.ID), fingerprint: stepFingerprint(step), address: place, job: job}
			if step.Pos != nil {
				node.position, node.hasPosition = *step.Pos, true
			}
			if parallel, ok := step.Exec.(*definition.ExecParallel); ok {
				node.children = walk(place+".parallel", job, parallel.Steps)
			}
			result = append(result, node)
		}
		return result
	}
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[id]
		if job == nil {
			continue
		}
		entry := skeletonJob{id: id, address: "jobs[" + strconv.Quote(id) + "]"}
		for _, pos := range []*sourcetext.Position{job.Pos, stringPos(job.ID)} {
			if pos != nil {
				entry.positions = append(entry.positions, *pos)
			}
		}
		address := entry.address + ".steps"
		if action {
			address = "runs.steps"
		}
		entry.steps = walk(address, stringValue(job.ID), job.Steps)
		built.jobs = append(built.jobs, entry)
	}
	return built
}

func (s skeleton) bind(path string, content []byte) References {
	index := sourcetext.NewReferenceIndex(path, content)
	refs := References{Document: index.Ref("", "document", "", "$"), Steps: map[*definition.Step]sourcetext.Reference{}, StepJobs: map[*definition.Step]string{}, Jobs: map[string]sourcetext.Reference{}, Positions: map[[2]int]sourcetext.Reference{}}
	var walk func(string, []skeletonStep)
	walk = func(owner string, steps []skeletonStep) {
		for _, step := range steps {
			ref := index.Ref(owner, step.key, step.fingerprint, step.address)
			refs.Steps[step.step] = ref
			refs.StepJobs[step.step] = step.job
			if step.hasPosition {
				refs.Positions[[2]int{step.position.Line, step.position.Column}] = ref
			}
			walk(ref.Node, step.children)
		}
	}
	for _, job := range s.jobs {
		refs.Jobs[job.id] = index.Ref("", "job:"+job.id, "", job.address)
		for _, pos := range job.positions {
			refs.Positions[[2]int{pos.Line, pos.Column}] = refs.Jobs[job.id]
		}
		walk("job:"+job.id, job.steps)
	}
	return refs
}

func stringPos(value *definition.String) *sourcetext.Position {
	if value == nil {
		return nil
	}
	return value.Pos
}

func stringValue(value *definition.String) string {
	if value == nil {
		return ""
	}
	return value.Value
}
