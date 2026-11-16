// Package configreport describes selected configuration as values, independently of
// configuration loading, validation and resolution.
package configreport

// Report describes the configuration actually selected for one project.
type Report struct {
	Project    string
	File       string
	Explicit   bool
	Overrides  []string
	Inspection Inspection
}

// Inspection contains effective settings and optional source locations.
type Inspection struct {
	Path     string            `json:"path" yaml:"path"`
	Config   map[string]any    `json:"config" yaml:"config"`
	Origins  map[string]Origin `json:"origins,omitempty" yaml:"origins,omitempty"`
	Warnings []Warning         `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// Origin identifies the default, project file, policy file or Action input that supplied a setting.
// Line and Column stay flat, rather than a source.Position, because they are
// part of the JSON and YAML report, where both are omitted when zero.
type Origin struct {
	File   string `json:"file,omitempty" yaml:"file,omitempty"`
	Source string `json:"source" yaml:"source"`
	Input  string `json:"input,omitempty" yaml:"input,omitempty"`
	State  string `json:"state" yaml:"state"`
	Line   int    `json:"line,omitempty" yaml:"line,omitempty"`
	Column int    `json:"column,omitempty" yaml:"column,omitempty"`
}

// Warning identifies an ignored setting without rejecting legacy config files.
// Like Origin, it keeps Line and Column flat for the JSON and YAML report.
type Warning struct {
	File    string `json:"file,omitempty" yaml:"file,omitempty"`
	Message string `json:"message" yaml:"message"`
	Line    int    `json:"line" yaml:"line"`
	Column  int    `json:"column" yaml:"column"`
}
