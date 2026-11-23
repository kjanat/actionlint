package outline

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
)

type WorkflowOutline struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	// ParseStatus is complete, partial (a tree with parser errors), or failed (no tree).
	ParseStatus string       `json:"parse_status"`
	Triggers    []string     `json:"triggers"`
	Jobs        []JobOutline `json:"jobs"`
}

type JobOutline struct {
	ID    string
	Name  string
	Start *sourcetext.Position
	Needs []string
	Uses  string
	Steps []StepOutline
}

// Reference is nil when the job calls no workflow.
func (j JobOutline) Reference() callreferences.UsesReference {
	return callreferences.ClassifyWorkflow(j.Uses)
}

type jobWire struct {
	ID        string                       `json:"id"`
	Name      string                       `json:"name,omitempty"`
	Start     *sourcetext.Position         `json:"start,omitempty"`
	Needs     []string                     `json:"needs"`
	Uses      string                       `json:"uses,omitempty"`
	Reference callreferences.UsesReference `json:"reference,omitempty"`
	Steps     []StepOutline                `json:"steps"`
}

func (j JobOutline) MarshalJSON() ([]byte, error) {
	return json.Marshal(jobWire{
		ID: j.ID, Name: j.Name, Start: j.Start, Needs: j.Needs,
		Uses: j.Uses, Reference: j.Reference(), Steps: j.Steps,
	})
}

func (j *JobOutline) UnmarshalJSON(data []byte) error {
	var value jobWire
	wire := struct {
		*jobWire
		Reference json.RawMessage `json:"reference"`
	}{jobWire: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if _, err := callreferences.DecodeUsesReference(wire.Reference); err != nil {
		return err
	}
	*j = JobOutline{ID: value.ID, Name: value.Name, Uses: value.Uses, Start: value.Start, Needs: value.Needs, Steps: value.Steps}
	return nil
}

const unknownStepKind = "unknown"

type StepOutline struct {
	ID    string
	Name  string
	Start *sourcetext.Position
	// Consumers of Kind must allow new kinds.
	Kind  string
	Uses  string
	Steps []StepOutline
}

// Reference is nil when the step uses no action.
func (s StepOutline) Reference() callreferences.UsesReference { return callreferences.Classify(s.Uses) }

type stepWire struct {
	ID        string                       `json:"id,omitempty"`
	Name      string                       `json:"name,omitempty"`
	Start     *sourcetext.Position         `json:"start,omitempty"`
	Kind      string                       `json:"kind"`
	Uses      string                       `json:"uses,omitempty"`
	Reference callreferences.UsesReference `json:"reference,omitempty"`
	Steps     []StepOutline                `json:"steps,omitempty"`
}

func (s StepOutline) MarshalJSON() ([]byte, error) {
	return json.Marshal(stepWire{
		ID: s.ID, Name: s.Name, Start: s.Start, Kind: s.Kind,
		Uses: s.Uses, Reference: s.Reference(), Steps: s.Steps,
	})
}

func (s *StepOutline) UnmarshalJSON(data []byte) error {
	var value stepWire
	wire := struct {
		*stepWire
		Reference json.RawMessage `json:"reference"`
	}{stepWire: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if _, err := callreferences.DecodeUsesReference(wire.Reference); err != nil {
		return err
	}
	*s = StepOutline{ID: value.ID, Name: value.Name, Kind: value.Kind, Uses: value.Uses, Start: value.Start, Steps: value.Steps}
	return nil
}
