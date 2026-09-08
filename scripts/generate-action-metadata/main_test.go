package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This small template schema exercises references, alternatives and inherited contexts.
const testSchema = `{
  "definitions": {
    "action-root": {"mapping": {"properties": {"inputs": "inputs", "runs": "runs"}}},
    "inputs": {"mapping": {"loose-value-type": "input"}},
    "input": {"mapping": {"properties": {"default": "default"}}},
    "default": {"context": ["github", "hashFiles(1,255)"], "string": {}},
    "runs": {"mapping": {"properties": {"steps": "steps"}}},
    "steps": {"sequence": {"item-type": "step"}},
    "step": {"context": ["github"], "one-of": ["run-step", "uses-step"]},
    "run-step": {"mapping": {"properties": {
      "run": {"type": "step-string", "required": true}, "shell": "step-string",
      "name": "step-string", "if": "step-if", "continue-on-error": "step-string",
      "working-directory": "step-string", "env": "step-map"
    }}},
    "uses-step": {"mapping": {"properties": {"uses": "string", "with": "step-map", "env": "step-map"}}},
    "step-string": {"context": ["inputs", "GITHUB", "hashFiles(1,255)"], "string": {}},
    "step-if": {"context": ["success(0,0)", "future(2,MAX)"], "string": {}},
    "step-map": {"context": ["env", "hashFiles(1,255)"], "mapping": {"loose-value-type": "string"}}
  }
}`

const testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCollect(t *testing.T) {
	defs, err := readSchema([]byte("\ufeff" + testSchema))
	if err != nil {
		t.Fatal(err)
	}
	tables, err := collect(defs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]availability{
		"inputs.*.default":    {[]string{"github"}, map[string]limits{"hashfiles": {1, 255}}},
		"runs.steps.*.run":    {[]string{"github", "inputs"}, map[string]limits{"hashfiles": {1, 255}}},
		"runs.steps.*.if":     {[]string{"github"}, map[string]limits{"success": {0, 0}, "future": {2, -1}}},
		"runs.steps.*.env.*":  {[]string{"env", "github"}, map[string]limits{"hashfiles": {1, 255}}},
		"runs.steps.*.with.*": {[]string{"env", "github"}, map[string]limits{"hashfiles": {1, 255}}},
	}
	for path, avail := range want {
		if diff := cmp.Diff(avail, tables.Availability[path]); diff != "" {
			t.Errorf("%s: %s", path, diff)
		}
	}
	if diff := cmp.Diff([]string{"env", "uses", "with"}, tables.Keys["uses-step"]); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff([]string{"future", "hashfiles", "success"}, tables.Functions); diff != "" {
		t.Error(diff)
	}
}

func TestGenerateRejectsSchemaDrift(t *testing.T) {
	tests := []struct{ name, from, to, message string }{
		{"missing root", "action-root", "renamed-root", "no action-root"},
		{"unknown reference", `"type": "step-string"`, `"type": "missing"`, "unknown schema definition"},
		{"cycle", `"item-type": "step"`, `"item-type": "steps"`, "recursive schema definition"},
		{"invalid property", `"type": "step-string"`, `"something": "step-string"`, "invalid property"},
		{"lost field", `"shell": "step-string"`, `"renamed-shell": "step-string"`, "required expression field"},
		{"lost contexts", `"default": "default"`, `"default": "string"`, "required expression field"},
		{"lost mapping", `"run-step"`, `"renamed-run-step"`, "closed step mapping"},
		{"bad function", "hashFiles(1,255)", "hashFiles(1)", "unsupported context entry"},
		{"inverted bounds", "hashFiles(1,255)", "hashFiles(3,2)", "invalid function limits"},
		{"conflicting alternatives", `"with": "step-map"`, `"if": "step-string", "with": "step-map"`, "conflicting context definitions"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.ReplaceAll(testSchema, tc.from, tc.to)
			if data == testSchema {
				t.Fatal("test did not alter schema")
			}
			_, err := generate([]byte(data), testRevision)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q, got %v", tc.message, err)
			}
		})
	}
}

func TestGenerateDeterministic(t *testing.T) {
	first, err := generate([]byte(testSchema), testRevision)
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal([]byte(testSchema), &schema); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate(reordered, testRevision)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("JSON property order changed generated tables")
	}
	formatted, err := format.Source(first)
	if err != nil || !bytes.Equal(first, formatted) {
		t.Fatalf("generated Go is not formatted: %v", err)
	}
	if !bytes.Contains(first, []byte("/blob/"+testRevision+"/"+schemaPath)) {
		t.Fatal("source revision missing from generated file")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRefresh(t *testing.T) {
	tests := []struct {
		name, commits, schema string
		apiStatus, cdnStatus  int
		fail                  bool
	}{
		{"latest schema", "valid", testSchema, 200, 200, false},
		{"API failure", "", "", 403, 200, true},
		{"bad JSON", "not JSON", "", 200, 200, true},
		{"no commits", "[]", "", 200, 200, true},
		{"invalid revision", `[{"sha":"main"}]`, "", 200, 200, true},
		{"CDN failure", "valid", "", 200, 503, true},
		{"invalid schema", "valid", "{}", 200, 200, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var apiCalled, cdnCalled bool
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				var data string
				var status int
				switch r.URL.Host {
				case "api.github.com":
					apiCalled = true
					if r.URL.Path != "/repos/"+repository+"/commits" || r.URL.Query().Get("path") != schemaPath || r.URL.Query().Get("per_page") != "1" {
						t.Fatalf("unexpected revision lookup: %s", r.URL)
					}
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Fatal("missing API authentication")
					}
					data, status = tc.commits, tc.apiStatus
					if data == "valid" {
						data = fmt.Sprintf(`[{"sha":%q}]`, testRevision)
					}
				case "cdn.jsdelivr.net":
					cdnCalled = true
					if r.URL.Path != "/gh/"+repository+"@"+testRevision+"/"+schemaPath {
						t.Fatalf("CDN did not use resolved commit: %s", r.URL)
					}
					if r.Header.Get("Authorization") != "" {
						t.Fatal("GitHub token sent to CDN")
					}
					data, status = tc.schema, tc.cdnStatus
				default:
					t.Fatalf("unexpected host: %s", r.URL)
				}
				return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(data))}, nil
			})}
			output := filepath.Join(t.TempDir(), "generated.go")
			if err := os.WriteFile(output, []byte("existing tables"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := refresh(t.Context(), client, "test-token", output)
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected refresh result: %v", err)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if tc.fail && string(got) != "existing tables" {
				t.Fatal("failed refresh overwrote existing tables")
			}
			if !apiCalled || !tc.fail && (!cdnCalled || !bytes.Contains(got, []byte(testRevision))) {
				t.Fatal("refresh did not fetch and record the resolved schema revision")
			}
		})
	}
}
