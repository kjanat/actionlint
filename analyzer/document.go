package analyzer

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
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
	Reference source.Reference    `json:"reference"`
	Uses      string              `json:"uses"`
	Kind      string              `json:"kind"`
	Start     diagnostic.Position `json:"start"`
	End       diagnostic.Position `json:"end"`
}
