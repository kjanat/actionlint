package scriptengine

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestDescriptorCloneIsolatesInputSelectors(t *testing.T) {
	t.Parallel()
	valid := checkinputs.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}
	descriptor := Descriptor{ScriptInputs: []checkinputs.ScriptInput{valid}}
	cloned := descriptor.Clone()
	cloned.ScriptInputs[0].Language = "python"
	if descriptor.ScriptInputs[0] != valid {
		t.Fatal("cloned input selectors alias descriptor")
	}
}

// The JSON form keeps the row/column member names engines already emit.
const diagnosticJSON = `{"filename":"a.sh","code":"SC1","message":"m","location":{"row":2,"column":3},"end_location":{"row":2,"column":5},` +
	`"template_effect":"none","fixes":[{"applicability":"safe","description":"d","replacements":[{"row":2,"end_row":2,"column":3,"end_column":5,"text":"x"}]}]}`

var diagnosticValue = Diagnostic{
	Filename: "a.sh", Code: "SC1", Message: "m",
	Location: sourcetext.Position{Line: 2, Column: 3}, EndLocation: sourcetext.Position{Line: 2, Column: 5},
	TemplateEffect: TemplateEffectNone,
	Fixes: []Fix{{Applicability: applicability.Safe, Description: "d", Replacements: []Replacement{{
		Start: sourcetext.Position{Line: 2, Column: 3}, End: sourcetext.Position{Line: 2, Column: 5}, Text: "x",
	}}}},
}

func TestDiagnosticJSONUsesRowMembers(t *testing.T) {
	t.Parallel()
	for name, unmarshal := range map[string]func([]byte, any) error{
		"encoding/json": json.Unmarshal,
		"encoding/json/v2": func(data []byte, v any) error {
			return jsonv2.Unmarshal(data, v)
		},
	} {
		var decoded []Diagnostic
		if err := unmarshal([]byte("["+diagnosticJSON+"]"), &decoded); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(decoded) != 1 || !reflect.DeepEqual(decoded[0], diagnosticValue) {
			t.Fatalf("%s decoded %+v, want %+v", name, decoded, diagnosticValue)
		}
	}
	for name, marshal := range map[string]func(any) ([]byte, error){
		"encoding/json": json.Marshal,
		"encoding/json/v2": func(v any) ([]byte, error) {
			return jsonv2.Marshal(v)
		},
	} {
		encoded, err := marshal(diagnosticValue)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(encoded) != diagnosticJSON {
			t.Fatalf("%s encoded\n%s\nwant\n%s", name, encoded, diagnosticJSON)
		}
	}
}

func TestDiagnosticJSONOmitsMissingEnd(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(Diagnostic{Filename: "a.sh", Code: "SC1", Message: "m", Location: sourcetext.Position{Line: 1, Column: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"filename":"a.sh","code":"SC1","message":"m","location":{"row":1,"column":1}}`; string(encoded) != want {
		t.Fatalf("encoded %s, want %s", encoded, want)
	}
}

func TestRequestEnvironmentJSON(t *testing.T) {
	t.Parallel()
	environments := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux},
		scripts.EnvironmentLayer{Scope: scripts.EnvironmentStep, Names: []string{"TOKEN"}})
	encoded, err := json.Marshal(Request{Source: "echo", Filename: "a.sh", Environment: environments})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"source":"echo","filename":"a.sh","environment":[{"operating_system":"linux","merge_comparison":"sensitive",` +
		`"lookup_comparison":"sensitive","names":[{"name":"TOKEN","scope":"step","case_known":true}],"unknown_names":false}]}`
	if string(encoded) != want {
		t.Fatalf("encoded\n%s\nwant\n%s", encoded, want)
	}
}
