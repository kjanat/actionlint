package inputs

import (
	"encoding/json"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

// DocumentRequest contains only captured document data and explicitly requested
// facts. Positions in dependencies use the captured document's YAML coordinates.
// Directory names the analysis root; it carries no filesystem handle.
type DocumentRequest struct {
	Source       string               `json:"source"`
	Filename     string               `json:"filename"`
	Kind         string               `json:"kind"`
	Config       json.RawMessage      `json:"config,omitempty"`
	Directory    string               `json:"directory,omitempty"`
	Dependencies []DocumentDependency `json:"dependencies,omitempty"`
}

// DocumentDependency explicitly maps a shared dependency into the process wire
// contract; the host never serializes analyzer.Input or its resource grants.
type DocumentDependency struct {
	Reference sourcepos.Reference `json:"reference"`
	Uses      string              `json:"uses"`
	Kind      string              `json:"kind"`
	Start     sourcepos.Position  `json:"start"`
	End       sourcepos.Position  `json:"end"`
}
