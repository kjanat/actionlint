package calls

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// metadataSummary renders the declarations of m so cases compare exactly,
// including the difference between an absent section and an empty one.
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

// TestParseMetadataParity pins what ParseMetadata reports for malformed,
// recovered and unusual reusable workflows. wantErr is a substring of the
// error; an empty wantErr means metadata is returned.
func TestParseMetadataParity(t *testing.T) {
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
		// required, default and type
		{name: "required without default", src: input("{type: string, required: true}"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required with default", src: input("{type: string, required: true, default: x}"), want: "in=[a:a/string] out=- sec=-"},
		{name: "required with null default", src: input("{type: string, required: true, default: null}"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required with empty default", src: input("{type: string, required: true, default: }"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required with number default", src: input("{type: number, required: true, default: 1}"), want: "in=[a:a/number] out=- sec=-"},
		{name: "required with false default", src: input("{type: boolean, required: true, default: false}"), want: "in=[a:a/bool] out=- sec=-"},
		{name: "mapping default", src: input("{type: string, required: true, default: {x: 1}}"), wantErr: "cannot construct !!map into string"},
		{name: "sequence default", src: input("{type: string, required: true, default: [1]}"), wantErr: "cannot construct !!seq into string"},
		{name: "required quoted yes", src: input("{type: string, required: 'yes'}"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "required expression", src: input("{type: string, required: '${{ x }}'}"), wantErr: "into bool"},
		{name: "required null", src: input("{type: string, required: }"), want: "in=[a:a/string] out=- sec=-"},
		{name: "required integer", src: input("{type: string, required: 1}"), wantErr: "into bool"},
		{name: "type any", src: input("{type: any}"), want: "in=[a:a/any] out=- sec=-"},
		{name: "type unknown", src: input("{type: array}"), want: "in=[a:a/any] out=- sec=-"},
		{name: "type wrong case", src: input("{type: String}"), want: "in=[a:a/any] out=- sec=-"},
		{name: "type empty", src: input("{type: ''}"), want: "in=[a:a/any] out=- sec=-"},
		{name: "type integer", src: input("{type: 1}"), want: "in=[a:a/any] out=- sec=-"},
		{name: "type mapping", src: input("{type: {x: 1}}"), wantErr: "cannot construct !!map into string"},
		{name: "type sequence", src: input("{type: [string]}"), wantErr: "cannot construct !!seq into string"},
		{name: "type missing keeps required", src: input("{required: true}"), want: "in=[a:a/any/req] out=- sec=-"},
		{name: "unknown and descriptive keys", src: input("{type: string, bogus: 1, description: d}"), want: "in=[a:a/string] out=- sec=-"},
		{name: "expression in description", src: input("{type: string, description: '${{ x }}'}"), want: "in=[a:a/string] out=- sec=-"},

		// input, secret and output shapes
		{name: "null input", src: input(""), want: "in=[a:a/any] out=- sec=-"},
		{name: "scalar input", src: input("foo"), wantErr: "cannot construct !!str `foo`"},
		{name: "sequence input", src: input("[x]"), wantErr: "cannot construct !!seq"},
		{name: "null secret", src: secret(""), want: "in=- out=- sec=[a:a]"},
		{name: "scalar secret", src: secret("foo"), wantErr: "cannot construct !!str `foo`"},
		{name: "sequence secret", src: secret("[x]"), wantErr: "cannot construct !!seq"},
		{name: "required quoted no secret", src: secret("{required: 'no'}"), want: "in=- out=- sec=[a:a]"},
		{name: "required secret keeps key spelling", src: call("    secrets:\n      A: {required: true, description: d, other: 1}\n"), want: "in=- out=- sec=[a:A/req]"},
		{name: "output without value", src: call("    outputs:\n      O: {description: d}\n"), want: "in=- out=[o:O] sec=-"},
		{name: "scalar output", src: call("    outputs:\n      O: foo\n"), want: "in=- out=[o:O] sec=-"},
		{name: "sequence output", src: call("    outputs:\n      O: [x]\n"), want: "in=- out=[o:O] sec=-"},
		{name: "mixed case output", src: call("    outputs:\n      MyOut: {value: x}\n"), want: "in=- out=[myout:MyOut] sec=-"},

		// sections: missing, null, empty and malformed
		{name: "null inputs", src: call("    inputs:\n"), want: none},
		{name: "empty inputs", src: call("    inputs: {}\n"), want: "in=[] out=- sec=-"},
		{name: "empty secrets", src: call("    secrets: {}\n"), want: "in=- out=- sec=[]"},
		{name: "empty outputs", src: call("    outputs: {}\n"), want: "in=- out=[] sec=-"},
		{name: "scalar inputs", src: call("    inputs: foo\n"), wantErr: "on.workflow_call.inputs must be mapping node but scalar node was found at line:3, col:13"},
		{name: "sequence inputs", src: call("    inputs: [a]\n"), wantErr: "on.workflow_call.inputs must be mapping node but sequence node was found at line:3, col:13"},
		{name: "scalar secrets", src: call("    secrets: foo\n"), wantErr: "on.workflow_call.secrets must be mapping node but scalar node was found at line:3, col:14"},
		{name: "scalar outputs", src: call("    outputs: foo\n"), wantErr: "on.workflow_call.outputs must be mapping node but scalar node was found at line:3, col:14"},
		{name: "duplicate inputs section", src: call("    inputs:\n      a: {type: string}\n    inputs:\n      b: {type: string}\n"), wantErr: `mapping key "inputs" already defined at line 3`},
		{name: "unknown call key", src: call("    bogus: 1\n    inputs:\n      a: {type: string}\n"), want: "in=[a:a/string] out=- sec=-"},
		{name: "section key is case sensitive", src: call("    INPUTS:\n      a: {type: string}\n"), want: none},
		{name: "case insensitive duplicate input", src: call("    inputs:\n      A: {type: string}\n      a: {type: number}\n"), want: "in=[a:a/number] out=- sec=-"},
		{name: "empty input name", src: call("    inputs:\n      '': {type: string}\n"), want: "in=[:/string] out=- sec=-"},
		{name: "numeric input name", src: call("    inputs:\n      1: {type: string}\n"), want: "in=[1:1/string] out=- sec=-"},
		{name: "merge key in input", src: call("    inputs:\n      a:\n        <<: {type: string, required: true}\n"), want: "in=[a:a/string/req] out=- sec=-"},
		{name: "tagged input name", src: call("    inputs:\n      !!str a: {type: string}\n"), want: "in=[a:a/string] out=- sec=-"},

		// locating the event
		{name: "scalar trigger", src: "on: workflow_call\n", want: none},
		{name: "upper case scalar trigger", src: "on: WORKFLOW_CALL\n", want: none},
		{name: "sequence trigger", src: "on: [push, workflow_call]\n", want: none},
		{name: "upper case sequence trigger", src: "on: [Workflow_Call]\n", want: none},
		{name: "upper case mapping trigger", src: "on:\n  Workflow_Call:\n    inputs:\n      a: {type: string}\n", want: "in=[a:a/string] out=- sec=-"},
		{name: "upper case on key", src: "On:\n  workflow_call:\n", wantErr: `"on:" is not found`},
		{name: "quoted on key", src: "\"on\":\n  workflow_call:\n    inputs:\n      a: {type: string}\n", want: "in=[a:a/string] out=- sec=-"},
		{name: "duplicate on keeps first", src: "on:\n  workflow_call:\n    inputs:\n      a: {type: string}\non: push\n", want: "in=[a:a/string] out=- sec=-"},
		{name: "duplicate on without call first", src: "on: push\non:\n  workflow_call:\n", wantErr: `"workflow_call" event trigger is not found in "on:" at line:1, column:5`},
		{name: "duplicate call keeps first", src: call("    inputs:\n      a: {type: string}\n  workflow_call:\n    inputs:\n      b: {type: string}\n"), want: "in=[a:a/string] out=- sec=-"},
		{name: "scalar call body", src: "on:\n  workflow_call: foo\n", wantErr: "cannot construct !!str `foo`"},
		{name: "sequence call body", src: "on:\n  workflow_call: [a]\n", wantErr: "cannot construct !!seq"},
		{name: "empty call body", src: "on:\n  workflow_call: {}\n", want: none},
		{name: "flow keys without values", src: "on:\n  workflow_call: {inputs, outputs}\n", want: none},
		{name: "aliased call body", src: "x: &c\n  inputs:\n    a: {type: string}\non:\n  workflow_call: *c\n", want: "in=[a:a/string] out=- sec=-"},

		// documents
		{name: "sequence document", src: "- a\n", wantErr: "cannot construct !!seq"},
		{name: "scalar document", src: "foo\n", wantErr: "cannot construct !!str `foo`"},
		{name: "null document", src: "~\n", wantErr: `"on:" is not found`},
		{name: "invalid yaml", src: "on: [\n", wantErr: "did not find expected node content"},
		{name: "byte order mark", src: "\xef\xbb\xbf" + input("{type: string}"), want: "in=[a:a/string] out=- sec=-"},
		{name: "second document", src: input("{type: string}") + "---\nfoo: bar\n", want: "in=[a:a/string] out=- sec=-"},

		// errors outside workflow_call do not fail the decoder
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
