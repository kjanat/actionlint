package document

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/diagnostic"
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
	ID        string               `json:"id"`
	Name      string               `json:"name,omitempty"`
	Start     *diagnostic.Position `json:"start,omitempty"`
	Needs     []string             `json:"needs"`
	Uses      string               `json:"uses,omitempty"`
	Reference UsesReference        `json:"reference,omitempty"`
	Steps     []Step               `json:"steps"`
}

// Step identifies a declared step without exporting scripts or parser internals.
// Kind is run, uses, wait, cancel, parallel, or unknown; consumers must allow new kinds.
// Steps contains the children of a parallel group, in declaration order.
type Step struct {
	ID        string               `json:"id,omitempty"`
	Name      string               `json:"name,omitempty"`
	Start     *diagnostic.Position `json:"start,omitempty"`
	Kind      string               `json:"kind"`
	Uses      string               `json:"uses,omitempty"`
	Reference UsesReference        `json:"reference,omitempty"`
	Steps     []Step               `json:"steps,omitempty"`
}

// UnmarshalJSON restores the typed reference without exposing parser internals.
func (j *Job) UnmarshalJSON(data []byte) error {
	type plain Job
	var value plain
	wire := struct {
		*plain
		Reference json.RawMessage `json:"reference"`
	}{plain: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	reference, err := decodeUsesReference(wire.Reference)
	if err != nil {
		return err
	}
	value.Reference = reference
	*j = Job(value)
	return nil
}

// UnmarshalJSON restores typed references and nested parallel steps.
func (s *Step) UnmarshalJSON(data []byte) error {
	type plain Step
	var value plain
	wire := struct {
		*plain
		Reference json.RawMessage `json:"reference"`
	}{plain: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	reference, err := decodeUsesReference(wire.Reference)
	if err != nil {
		return err
	}
	value.Reference = reference
	*s = Step(value)
	return nil
}
