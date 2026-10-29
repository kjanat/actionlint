package document

import (
	"encoding/json"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
)

// Workflow describes selected workflow declarations recovered by a parser.
// ParseStatus is complete, partial (a tree with parser errors), or failed (no tree).
// A complete parse can still produce validity findings or an analysis failure.
// A Workflow is an immutable value; accessors return detached copies.
type Workflow struct {
	path, name, parseStatus string
	triggers                []string
	jobs                    []Job
}

// WorkflowFields lists the declarations recorded by NewWorkflow.
type WorkflowFields struct {
	Path        string
	Name        string
	ParseStatus string
	Triggers    []string
	Jobs        []Job
}

// NewWorkflow records fields; later changes to their slices do not affect it.
func NewWorkflow(fields WorkflowFields) Workflow {
	return Workflow{
		path: fields.Path, name: fields.Name, parseStatus: fields.ParseStatus,
		triggers: slices.Clone(fields.Triggers), jobs: cloneJobs(fields.Jobs),
	}
}

// Name returns the declared workflow name.
func (w Workflow) Name() string { return w.name }

// ParseStatus returns complete, partial or failed.
func (w Workflow) ParseStatus() string { return w.parseStatus }

// Triggers returns the declared event names in source order.
func (w Workflow) Triggers() []string { return slices.Clone(w.triggers) }

// Jobs returns the jobs in source order.
func (w Workflow) Jobs() []Job { return cloneJobs(w.jobs) }

type workflowWire struct {
	Path        string   `json:"path"`
	Name        string   `json:"name,omitempty"`
	ParseStatus string   `json:"parse_status"`
	Triggers    []string `json:"triggers"`
	Jobs        []Job    `json:"jobs"`
}

// Job describes a workflow job.
// Jobs appear in source order. Start points to the job key, when available.
type Job struct {
	id, name, uses string
	start          *diagnostic.Position
	needs          []string
	steps          []Step
}

// JobFields lists the declarations recorded by NewJob.
type JobFields struct {
	ID    string
	Name  string
	Start *diagnostic.Position
	Needs []string
	Uses  string
	Steps []Step
}

// NewJob records fields; later changes to their storage do not affect it.
func NewJob(fields JobFields) Job {
	return Job{
		id: fields.ID, name: fields.Name, uses: fields.Uses, start: clonePosition(fields.Start),
		needs: slices.Clone(fields.Needs), steps: cloneSteps(fields.Steps),
	}
}

// ID returns the job key.
func (j Job) ID() string { return j.id }

// Name returns the declared job name.
func (j Job) Name() string { return j.name }

// Start returns a copy of the job key position, or nil when unknown.
func (j Job) Start() *diagnostic.Position { return clonePosition(j.start) }

// Needs returns the declared job dependencies.
func (j Job) Needs() []string { return slices.Clone(j.needs) }

// Uses returns the reusable workflow declaration, if any.
func (j Job) Uses() string { return j.uses }

// Reference classifies Uses with ParseWorkflowReference; it is nil when the job
// calls no workflow.
func (j Job) Reference() UsesReference { return ParseWorkflowReference(j.uses) }

// Steps returns the declared steps in source order.
func (j Job) Steps() []Step { return cloneSteps(j.steps) }

type jobWire struct {
	ID        string               `json:"id"`
	Name      string               `json:"name,omitempty"`
	Start     *diagnostic.Position `json:"start,omitempty"`
	Needs     []string             `json:"needs"`
	Uses      string               `json:"uses,omitempty"`
	Reference UsesReference        `json:"reference,omitempty"`
	Steps     []Step               `json:"steps"`
}

// MarshalJSON encodes the job's declarations.
func (j Job) MarshalJSON() ([]byte, error) {
	return marshal(jobWire{
		ID: j.id, Name: j.name, Start: clonePosition(j.start), Needs: slices.Clone(j.needs),
		Uses: j.uses, Reference: ParseWorkflowReference(j.uses), Steps: cloneSteps(j.steps),
	})
}

// UnmarshalJSON validates the encoded reference; Reference derives it from uses.
func (j *Job) UnmarshalJSON(data []byte) error {
	var value jobWire
	wire := struct {
		*jobWire
		Reference json.RawMessage `json:"reference"`
	}{jobWire: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if _, err := decodeUsesReference(wire.Reference); err != nil {
		return err
	}
	*j = Job{id: value.ID, name: value.Name, uses: value.Uses, start: value.Start, needs: value.Needs, steps: value.Steps}
	return nil
}

// Step identifies a declared step without exporting scripts or parser internals.
// Kind is run, uses, wait, cancel, parallel, or unknown; consumers must allow new kinds.
// Steps contains the children of a parallel group, in declaration order.
type Step struct {
	id, name, kind, uses string
	start                *diagnostic.Position
	steps                []Step
}

// StepFields lists the declarations recorded by NewStep.
type StepFields struct {
	ID    string
	Name  string
	Start *diagnostic.Position
	Kind  string
	Uses  string
	Steps []Step
}

// NewStep records fields; later changes to their storage do not affect it.
func NewStep(fields StepFields) Step {
	return Step{
		id: fields.ID, name: fields.Name, kind: fields.Kind, uses: fields.Uses,
		start: clonePosition(fields.Start), steps: cloneSteps(fields.Steps),
	}
}

// ID returns the declared step id.
func (s Step) ID() string { return s.id }

// Name returns the declared step name.
func (s Step) Name() string { return s.name }

// Start returns a copy of the step position, or nil when unknown.
func (s Step) Start() *diagnostic.Position { return clonePosition(s.start) }

// Kind returns run, uses, wait, cancel, parallel or unknown.
func (s Step) Kind() string { return s.kind }

// Uses returns the declared action, if any.
func (s Step) Uses() string { return s.uses }

// Reference classifies Uses with ParseReference; it is nil when the step uses
// no action.
func (s Step) Reference() UsesReference { return ParseReference(s.uses) }

// Steps returns the children of a parallel group.
func (s Step) Steps() []Step { return cloneSteps(s.steps) }

type stepWire struct {
	ID        string               `json:"id,omitempty"`
	Name      string               `json:"name,omitempty"`
	Start     *diagnostic.Position `json:"start,omitempty"`
	Kind      string               `json:"kind"`
	Uses      string               `json:"uses,omitempty"`
	Reference UsesReference        `json:"reference,omitempty"`
	Steps     []Step               `json:"steps,omitempty"`
}

// MarshalJSON encodes the step's declarations.
func (s Step) MarshalJSON() ([]byte, error) {
	return marshal(stepWire{
		ID: s.id, Name: s.name, Start: clonePosition(s.start), Kind: s.kind,
		Uses: s.uses, Reference: ParseReference(s.uses), Steps: cloneSteps(s.steps),
	})
}

// UnmarshalJSON validates the encoded reference and restores nested parallel
// steps; Reference derives the reference from uses.
func (s *Step) UnmarshalJSON(data []byte) error {
	var value stepWire
	wire := struct {
		*stepWire
		Reference json.RawMessage `json:"reference"`
	}{stepWire: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if _, err := decodeUsesReference(wire.Reference); err != nil {
		return err
	}
	*s = Step{id: value.ID, name: value.Name, kind: value.Kind, uses: value.Uses, start: value.Start, steps: value.Steps}
	return nil
}

func clonePosition(position *diagnostic.Position) *diagnostic.Position {
	if position == nil {
		return nil
	}
	copied := *position
	return &copied
}

func cloneJobs(jobs []Job) []Job {
	if jobs == nil {
		return nil
	}
	out := make([]Job, len(jobs))
	for i, job := range jobs {
		out[i] = Job{
			id: job.id, name: job.name, uses: job.uses, start: clonePosition(job.start),
			needs: slices.Clone(job.needs), steps: cloneSteps(job.steps),
		}
	}
	return out
}

func cloneSteps(steps []Step) []Step {
	if steps == nil {
		return nil
	}
	out := make([]Step, len(steps))
	for i, step := range steps {
		out[i] = Step{
			id: step.id, name: step.name, kind: step.kind, uses: step.uses,
			start: clonePosition(step.start), steps: cloneSteps(step.steps),
		}
	}
	return out
}
