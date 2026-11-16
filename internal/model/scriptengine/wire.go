package scriptengine

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
)

// wireDiagnostic is the JSON form of Diagnostic. Core JSON engines, native
// process engines and plugin authoring fixtures spell positions as
// {"row", "column"} and replacements as row/end_row/column/end_column, so
// those names exist only here.
type wireDiagnostic struct {
	Filename       string         `json:"filename"`
	Code           string         `json:"code"`
	Name           string         `json:"name,omitempty"`
	Message        string         `json:"message"`
	Severity       string         `json:"severity,omitempty"`
	Location       wirePosition   `json:"location"`
	EndLocation    wirePosition   `json:"end_location,omitzero"`
	URL            string         `json:"url,omitempty"`
	TemplateEffect TemplateEffect `json:"template_effect,omitempty"`
	Source         string         `json:"source,omitempty"`
	Fixes          []wireFix      `json:"fixes,omitempty"`
}

type wirePosition struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

type wireFix struct {
	Applicability applicability.Applicability `json:"applicability,omitempty"`
	Description   string                      `json:"description,omitempty"`
	Replacements  []wireReplacement           `json:"replacements"`
}

type wireReplacement struct {
	Row       int    `json:"row"`
	EndRow    int    `json:"end_row"`
	Column    int    `json:"column"`
	EndColumn int    `json:"end_column"`
	Text      string `json:"text"`
}

func newWirePosition(p sourcetext.Position) wirePosition {
	return wirePosition{Row: p.Line, Column: p.Column}
}

func (p wirePosition) position() sourcetext.Position {
	return sourcetext.Position{Line: p.Row, Column: p.Column}
}

func newWireDiagnostic(d Diagnostic) wireDiagnostic {
	wire := wireDiagnostic{
		Filename: d.Filename, Code: d.Code, Name: d.Name, Message: d.Message, Severity: d.Severity,
		Location: newWirePosition(d.Location), EndLocation: newWirePosition(d.EndLocation),
		URL: d.URL, TemplateEffect: d.TemplateEffect, Source: d.Source,
	}
	if d.Fixes != nil {
		wire.Fixes = make([]wireFix, len(d.Fixes))
	}
	for i, fix := range d.Fixes {
		wire.Fixes[i] = wireFix{Applicability: fix.Applicability, Description: fix.Description}
		if fix.Replacements != nil {
			wire.Fixes[i].Replacements = make([]wireReplacement, len(fix.Replacements))
		}
		for j, r := range fix.Replacements {
			wire.Fixes[i].Replacements[j] = wireReplacement{Row: r.Start.Line, Column: r.Start.Column, EndRow: r.End.Line, EndColumn: r.End.Column, Text: r.Text}
		}
	}
	return wire
}

func (wire wireDiagnostic) diagnostic() Diagnostic {
	d := Diagnostic{
		Filename: wire.Filename, Code: wire.Code, Name: wire.Name, Message: wire.Message, Severity: wire.Severity,
		Location: wire.Location.position(), EndLocation: wire.EndLocation.position(),
		URL: wire.URL, TemplateEffect: wire.TemplateEffect, Source: wire.Source,
	}
	if wire.Fixes != nil {
		d.Fixes = make([]Fix, len(wire.Fixes))
	}
	for i, fix := range wire.Fixes {
		d.Fixes[i] = Fix{Applicability: fix.Applicability, Description: fix.Description}
		if fix.Replacements != nil {
			d.Fixes[i].Replacements = make([]Replacement, len(fix.Replacements))
		}
		for j, r := range fix.Replacements {
			d.Fixes[i].Replacements[j] = Replacement{
				Start: sourcetext.Position{Line: r.Row, Column: r.Column},
				End:   sourcetext.Position{Line: r.EndRow, Column: r.EndColumn},
				Text:  r.Text,
			}
		}
	}
	return d
}

// MarshalJSON writes the JSON form; encoding/json/v2 uses it too.
func (d Diagnostic) MarshalJSON() ([]byte, error) {
	return json.Marshal(newWireDiagnostic(d))
}

// UnmarshalJSON reads the JSON form; encoding/json/v2 uses it too.
func (d *Diagnostic) UnmarshalJSON(data []byte) error {
	var wire wireDiagnostic
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*d = wire.diagnostic()
	return nil
}
