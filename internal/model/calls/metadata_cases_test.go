package calls

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func metadataSummary(m *Metadata) string {
	section := func(entries []string, present bool) string {
		if !present {
			return "-"
		}
		slices.Sort(entries)
		return "[" + strings.Join(entries, ",") + "]"
	}
	var inputs, outputs, secrets []string
	for key, in := range m.Inputs {
		entry := fmt.Sprintf("%s:%s/%s", key, in.Name, in.Type)
		if in.Required {
			entry += "/req"
		}
		inputs = append(inputs, entry)
	}
	for key, out := range m.Outputs {
		outputs = append(outputs, key+":"+out.Name)
	}
	for key, secret := range m.Secrets {
		entry := key + ":" + secret.Name
		if secret.Required {
			entry += "/req"
		}
		secrets = append(secrets, entry)
	}
	summary := fmt.Sprintf("in=%s out=%s sec=%s",
		section(inputs, m.Inputs != nil), section(outputs, m.Outputs != nil), section(secrets, m.Secrets != nil))
	if len(m.JobPermissions) > 0 {
		summary += fmt.Sprintf(" perm=%v", m.JobPermissions)
	}
	return summary
}

func TestParseMetadataCases(t *testing.T) {
	t.Parallel()
	const none = "in=- out=- sec=-"
	call := func(body string) string { return "on:\n  workflow_call:\n" + body }
	input := func(def string) string { return call("    inputs:\n      a: " + def + "\n") }
	secret := func(def string) string { return call("    secrets:\n      a: " + def + "\n") }
	tests := []struct {
		name    string
		src     string
		want    string
		wantErr string
	}{
		{name: "required without default", src: input("{type: string, required: true}"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required with default", src: input("{type: string, required: true, default: x}"), want: "in=[a:a/string] out=- sec=-"},
		{name: "required with null default", src: input("{type: string, required: true, default: null}"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required with empty default", src: input("{type: string, required: true, default: }"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required with number default", src: input("{type: number, required: true, default: 1}"), want: "in=[a:a/number] out=- sec=-"},
		{name: "required with false default", src: input("{type: boolean, required: true, default: false}"), want: "in=[a:a/bool] out=- sec=-"},
		{name: "mapping default", src: input("{type: string, required: true, default: {x: 1}}"), wantErr: "line:4, column:50: expected scalar node for string value but found mapping node"},
		{name: "sequence default", src: input("{type: string, required: true, default: [1]}"), wantErr: "line:4, column:50: expected scalar node for string value but found sequence node"},
		{name: "required quoted yes", src: input("{type: string, required: 'yes'}"), wantErr: `expected boolean literal but found scalar node with "!!str" tag`},
		{name: "required expression", src: input("{type: string, required: '${{ x }}'}"), wantErr: `expected boolean literal but found scalar node with "!!str" tag`},
		{name: "required null", src: input("{type: string, required: }"), wantErr: `expected boolean literal but found scalar node with "!!null" tag`},
		{name: "required integer", src: input("{type: string, required: 1}"), wantErr: `expected boolean literal but found scalar node with "!!int" tag`},
		{name: "type any", src: input("{type: any}"), wantErr: `line:4, column:17: invalid value "any" for input type of workflow_call event`},
		{name: "type unknown", src: input("{type: array}"), wantErr: `invalid value "array" for input type`},
		{name: "type wrong case", src: input("{type: String}"), wantErr: `invalid value "String" for input type`},
		{name: "type empty", src: input("{type: ''}"), wantErr: "line:4, column:17: string should not be empty"},
		{name: "type integer", src: input("{type: 1}"), wantErr: `invalid value "1" for input type`},
		{name: "type mapping", src: input("{type: {x: 1}}"), wantErr: "expected scalar node for string value but found mapping node"},
		{name: "type sequence", src: input("{type: [string]}"), wantErr: "expected scalar node for string value but found sequence node"},
		{name: "type missing", src: input("{required: true}"), wantErr: `line:4, column:7: "type" is missing at "a" input of workflow_call event`},
		{name: "unknown input key", src: input("{type: string, bogus: 1, description: d}"), wantErr: `unexpected key "bogus" for inputs at workflow_call event`},
		{name: "expression in description", src: input("{type: string, description: '${{ x }}'}"), want: "in=[a:a/string] out=- sec=-"},

		{name: "null input", src: input(""), wantErr: `"type" is missing at "a" input of workflow_call event`},
		{name: "scalar input", src: input("foo"), wantErr: "line:4, column:10: input of workflow_call event is scalar node but mapping node is expected"},
		{name: "sequence input", src: input("[x]"), wantErr: "input of workflow_call event is sequence node but mapping node is expected"},
		{name: "null secret", src: secret(""), want: "in=- out=- sec=[a:a]"},
		{name: "scalar secret", src: secret("foo"), wantErr: "secret of workflow_call event is scalar node but mapping node is expected"},
		{name: "sequence secret", src: secret("[x]"), wantErr: "secret of workflow_call event is sequence node but mapping node is expected"},
		{name: "required quoted no secret", src: secret("{required: 'no'}"), wantErr: `expected boolean literal but found scalar node with "!!str" tag`},
		{name: "required secret keeps key spelling", src: call("    secrets:\n      A: {required: true, description: d}\n"), want: "in=- out=- sec=[a:A/req]"},
		{name: "unknown secret key", src: secret("{required: true, other: 1}"), wantErr: `unexpected key "other" for "secrets" section`},
		{name: "output without value", src: call("    outputs:\n      O: {description: d}\n"), wantErr: `line:4, column:7: "value" is missing at "O" output of workflow_call event`},
		{name: "scalar output", src: call("    outputs:\n      O: foo\n"), wantErr: "output of workflow_call event is scalar node but mapping node is expected"},
		{name: "sequence output", src: call("    outputs:\n      O: [x]\n"), wantErr: "output of workflow_call event is sequence node but mapping node is expected"},
		{name: "mixed case output", src: call("    outputs:\n      MyOut: {value: x}\n"), want: "in=- out=[myout:MyOut] sec=-"},

		{name: "null inputs", src: call("    inputs:\n"), want: "in=[] out=- sec=-"},
		{name: "empty inputs", src: call("    inputs: {}\n"), want: "in=[] out=- sec=-"},
		{name: "empty secrets", src: call("    secrets: {}\n"), want: "in=- out=- sec=[]"},
		{name: "empty outputs", src: call("    outputs: {}\n"), want: "in=- out=[] sec=-"},
		{name: "scalar inputs", src: call("    inputs: foo\n"), wantErr: `line:3, column:13: "inputs" section is scalar node but mapping node is expected`},
		{name: "sequence inputs", src: call("    inputs: [a]\n"), wantErr: `line:3, column:13: "inputs" section is sequence node but mapping node is expected`},
		{name: "scalar secrets", src: call("    secrets: foo\n"), wantErr: `line:3, column:14: "secrets" section is scalar node but mapping node is expected`},
		{name: "scalar outputs", src: call("    outputs: foo\n"), wantErr: `line:3, column:14: "outputs" section is scalar node but mapping node is expected`},
		{name: "duplicate inputs section", src: call("    inputs:\n      a: {type: string}\n    inputs:\n      b: {type: string}\n"), wantErr: `line:5, column:5: key "inputs" is duplicated in "workflow_call" section`},
		{name: "unknown call key", src: call("    bogus: 1\n    inputs:\n      a: {type: string}\n"), wantErr: `line:3, column:5: unexpected key "bogus" for "workflow_call" section`},
		{name: "section key is case sensitive", src: call("    INPUTS:\n      a: {type: string}\n"), wantErr: `unexpected key "INPUTS" for "workflow_call" section`},
		{name: "case insensitive duplicate input", src: call("    inputs:\n      A: {type: string}\n      a: {type: number}\n"), wantErr: `line:5, column:7: key "a" is duplicated in "inputs" section`},
		{name: "empty input name", src: call("    inputs:\n      '': {type: string}\n"), wantErr: "line:4, column:7: string should not be empty"},
		{name: "numeric input name", src: call("    inputs:\n      1: {type: string}\n"), want: "in=[1:1/string] out=- sec=-"},
		{name: "merge key in input", src: call("    inputs:\n      a:\n        <<: {type: string, required: true}\n"), wantErr: `line:5, column:9: GitHub Actions does not support YAML merge key "<<"`},
		{name: "tagged input name", src: call("    inputs:\n      !!str a: {type: string}\n"), want: "in=[a:a/string] out=- sec=-"},

		{name: "scalar trigger", src: "on: workflow_call\n", want: none},
		{name: "upper case scalar trigger", src: "on: WORKFLOW_CALL\n", wantErr: `"workflow_call" event trigger is not found in "on:" at line:1, column:1`},
		{name: "sequence trigger", src: "on: [push, workflow_call]\n", want: none},
		{name: "upper case sequence trigger", src: "on: [Workflow_Call]\n", wantErr: `"workflow_call" event trigger is not found in "on:" at line:1, column:1`},
		{name: "upper case mapping trigger", src: "on:\n  Workflow_Call:\n    inputs:\n      a: {type: string}\n", wantErr: `"workflow_call" event trigger is not found in "on:" at line:1, column:1`},
		{name: "upper case on key", src: "On:\n  workflow_call:\n", wantErr: `"on:" is not found`},
		{name: "quoted on key", src: "\"on\":\n  workflow_call:\n    inputs:\n      a: {type: string}\n", want: "in=[a:a/string] out=- sec=-"},
		{name: "duplicate on keeps first", src: "on:\n  workflow_call:\n    inputs:\n      a: {type: string}\non: push\n", want: "in=[a:a/string] out=- sec=-"},
		{name: "duplicate on without call first", src: "on: push\non:\n  workflow_call:\n", wantErr: `"workflow_call" event trigger is not found in "on:" at line:1, column:1`},
		{name: "duplicate call keeps first", src: call("    inputs:\n      a: {type: string}\n  workflow_call:\n    inputs:\n      b: {type: string}\n"), want: "in=[a:a/string] out=- sec=-"},
		{name: "scalar call body", src: "on:\n  workflow_call: foo\n", wantErr: `line:2, column:18: "workflow_call" section is scalar node but mapping node is expected`},
		{name: "sequence call body", src: "on:\n  workflow_call: [a]\n", wantErr: `line:2, column:18: "workflow_call" section is sequence node but mapping node is expected`},
		{name: "empty call body", src: "on:\n  workflow_call: {}\n", want: none},
		{name: "flow keys without values", src: "on:\n  workflow_call: {inputs, outputs}\n", want: "in=[] out=[] sec=-"},
		{name: "aliased call body", src: "x: &c\n  inputs:\n    a: {type: string}\non:\n  workflow_call: *c\n", want: "in=[a:a/string] out=- sec=-"},

		{name: "sequence document", src: "- a\n", wantErr: `"on:" is not found`},
		{name: "scalar document", src: "foo\n", wantErr: `"on:" is not found`},
		{name: "null document", src: "~\n", wantErr: `"on:" is not found`},
		{name: "invalid yaml", src: "on: [\n", wantErr: "did not find expected node content"},
		{name: "byte order mark", src: "\xef\xbb\xbf" + input("{type: string}"), want: "in=[a:a/string] out=- sec=-"},
		{name: "second document", src: input("{type: string}") + "---\nfoo: bar\n", want: "in=[a:a/string] out=- sec=-"},

		{name: "jobs missing", src: "on: workflow_call\n", want: none},
		{name: "unknown top level key", src: "on: workflow_call\nbogus: 1\n", want: none},
		{name: "invalid jobs", src: "on: workflow_call\njobs: foo\n", want: none},
		{name: "job permissions are kept", src: "on: workflow_call\npermissions: {contents: read}\njobs:\n  a:\n    runs-on: x\n    steps: [{run: x}]\n", want: "in=- out=- sec=- perm=map[a:map[contents:read]]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMetadata([]byte(tc.src))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("metadata = %+v alongside error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if summary := metadataSummary(got); summary != tc.want {
				t.Fatalf("metadata = %s, want %s", summary, tc.want)
			}
		})
	}
}
