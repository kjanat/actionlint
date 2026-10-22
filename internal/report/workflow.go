package report

import "encoding/json"

// WorkflowOutline describes selected workflow declarations recovered by Parse.
// ParseStatus is complete, partial (a tree with parser errors), or failed (no tree).
// A complete parse can still produce validity findings or an analysis failure.
type WorkflowOutline struct {
	Path        string       `json:"path"`
	Name        string       `json:"name,omitempty"`
	ParseStatus string       `json:"parse_status"`
	Triggers    []string     `json:"triggers"`
	Jobs        []JobOutline `json:"jobs"`
}

// JobOutline retains declared IDs and references, including unresolved expressions.
// Jobs appear in source order. Start points to the job key, when available.
type JobOutline struct {
	ID        string              `json:"id"`
	Name      string              `json:"name,omitempty"`
	Start     *DiagnosticPosition `json:"start,omitempty"`
	Needs     []string            `json:"needs"`
	Uses      string              `json:"uses,omitempty"`
	Reference UsesReference       `json:"reference,omitempty"`
	Steps     []StepOutline       `json:"steps"`
}

// StepOutline identifies a declared step without exporting scripts or parser internals.
// Kind is run, uses, wait, cancel, parallel, or unknown; consumers must allow new kinds.
// Steps contains the children of a parallel group, in declaration order.
type StepOutline struct {
	ID        string              `json:"id,omitempty"`
	Name      string              `json:"name,omitempty"`
	Start     *DiagnosticPosition `json:"start,omitempty"`
	Kind      string              `json:"kind"`
	Uses      string              `json:"uses,omitempty"`
	Reference UsesReference       `json:"reference,omitempty"`
	Steps     []StepOutline       `json:"steps,omitempty"`
}

// UnmarshalJSON restores the typed reference without exposing parser internals.
func (j *JobOutline) UnmarshalJSON(data []byte) error {
	type plain JobOutline
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
	*j = JobOutline(value)
	return nil
}

// UnmarshalJSON restores typed references and nested parallel steps.
func (s *StepOutline) UnmarshalJSON(data []byte) error {
	type plain StepOutline
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
	*s = StepOutline(value)
	return nil
}
