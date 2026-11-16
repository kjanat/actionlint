package outline

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
)

// Workflow describes selected workflow declarations recovered by a parser.
// ParseStatus is complete, partial (a tree with parser errors), or failed (no tree).
// A complete parse can still produce validity findings or an analysis failure.
type Workflow struct {
	Path        string   `json:"path"`
	Name        string   `json:"name,omitempty"`
	ParseStatus string   `json:"parse_status"`
	Triggers    []string `json:"triggers"`
	Jobs        []Job    `json:"jobs"`
}

// Job describes a workflow job.
// Jobs appear in source order. Start points to the job key, when available.
type Job struct {
	ID    string
	Name  string
	Start *source.Position
	Needs []string
	Uses  string
	Steps []Step
}

// Reference classifies Uses with callrefs.ClassifyWorkflow; it is nil when the job
// calls no workflow.
func (j Job) Reference() references.UsesReference { return references.ClassifyWorkflow(j.Uses) }

type jobWire struct {
	ID        string                   `json:"id"`
	Name      string                   `json:"name,omitempty"`
	Start     *source.Position         `json:"start,omitempty"`
	Needs     []string                 `json:"needs"`
	Uses      string                   `json:"uses,omitempty"`
	Reference references.UsesReference `json:"reference,omitempty"`
	Steps     []Step                   `json:"steps"`
}

// MarshalJSON encodes the job's declarations.
func (j Job) MarshalJSON() ([]byte, error) {
	return json.Marshal(jobWire{
		ID: j.ID, Name: j.Name, Start: j.Start, Needs: j.Needs,
		Uses: j.Uses, Reference: j.Reference(), Steps: j.Steps,
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
	if _, err := references.DecodeUsesReference(wire.Reference); err != nil {
		return err
	}
	*j = Job{ID: value.ID, Name: value.Name, Uses: value.Uses, Start: value.Start, Needs: value.Needs, Steps: value.Steps}
	return nil
}

// Step identifies a declared step without exporting scripts or parser internals.
// Kind is run, uses, wait, cancel, parallel, or unknown; consumers must allow new kinds.
// Steps contains the children of a parallel group, in declaration order.
type Step struct {
	ID    string
	Name  string
	Start *source.Position
	Kind  string
	Uses  string
	Steps []Step
}

// Reference classifies Uses with callrefs.Classify; it is nil when the step uses
// no action.
func (s Step) Reference() references.UsesReference { return references.Classify(s.Uses) }

type stepWire struct {
	ID        string                   `json:"id,omitempty"`
	Name      string                   `json:"name,omitempty"`
	Start     *source.Position         `json:"start,omitempty"`
	Kind      string                   `json:"kind"`
	Uses      string                   `json:"uses,omitempty"`
	Reference references.UsesReference `json:"reference,omitempty"`
	Steps     []Step                   `json:"steps,omitempty"`
}

// MarshalJSON encodes the step's declarations.
func (s Step) MarshalJSON() ([]byte, error) {
	return json.Marshal(stepWire{
		ID: s.ID, Name: s.Name, Start: s.Start, Kind: s.Kind,
		Uses: s.Uses, Reference: s.Reference(), Steps: s.Steps,
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
	if _, err := references.DecodeUsesReference(wire.Reference); err != nil {
		return err
	}
	*s = Step{ID: value.ID, Name: value.Name, Kind: value.Kind, Uses: value.Uses, Start: value.Start, Steps: value.Steps}
	return nil
}
