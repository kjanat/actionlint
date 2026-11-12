package results_test

import (
	"encoding/json"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestActionMetadataReportContract(t *testing.T) {
	schemaBytes, err := os.ReadFile("schemas/results/v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument any
	if err := json.Unmarshal(schemaBytes, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	const schemaURL = "https://example.com/actionlint-result.schema.json"
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}

	request, err := actions.ParseReference("github.com", "owner/repository@v1")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity("github.com", "owner/repository", 42, "0123456789abcdef0123456789abcdef01234567", "")
	if err != nil {
		t.Fatal(err)
	}
	route, err := actions.RefRoute("refs/tags/v1")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(request, identity, "0123456789abcdef0123456789abcdef01234567", route, "2026-10-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifestFile(selection, "action.yml", []byte("name: Example\n"))
	if err != nil {
		t.Fatal(err)
	}
	problem := actions.ManifestProblem{Code: "invalid-field-type", Message: "metadata unavailable", Path: "action.yml", Start: sourcepos.Position{}, End: sourcepos.Position{}}
	reports := []actions.MetadataReport{
		(actions.Coverage{Reference: "owner/repository@v1", Availability: actions.MissingMetadata(availability.Offline())}).Report(),
		(actions.Coverage{Reference: "owner/repository@v1", Availability: actions.AvailableMetadata(manifest)}).Report(),
		(actions.Coverage{Reference: "owner/repository@v1", Availability: actions.InvalidMetadata(manifest, []actions.ManifestProblem{problem})}).Report(),
	}

	for _, report := range reports {
		data, err := json.Marshal(map[string]any{
			"schema_version": 1, "completed": true, "status": "success", "exit_code": 0,
			"file_count": 0, "diagnostics": []any{}, "configurations": []any{}, "hints": []any{},
			"action_metadata": []actions.MetadataReport{report},
		})
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%s report violates the result schema: %v\n%s", report.State, err, data)
		}
	}

	const resultPrefix = `{"schema_version":1,"completed":true,"status":"success","exit_code":0,"file_count":0,"diagnostics":[],"configurations":[],"hints":[],"action_metadata":[`
	const resultSuffix = `]}`
	for _, malformedRecord := range []string{
		`{"reference":"owner/repository@v1","state":"unknown"}`,
		`{"reference":"owner/repository@v1","state":"unavailable","reason":"invented"}`,
		`{"reference":"owner/repository@v1","state":"available","host":"github.com","repository":"owner/repository","repository_id":42,"commit":"0123456789abcdef0123456789abcdef01234567","manifest":"action.yml","selected_object":"0123456789abcdef0123456789abcdef01234567","reason":"offline"}`,
		`{"reference":"owner/repository@v1","state":"invalid","host":"github.com","repository":"owner/repository","repository_id":42,"commit":"0123456789abcdef0123456789abcdef01234567","manifest":"action.yml","selected_object":"0123456789abcdef0123456789abcdef01234567","problems":[]}`,
		`{"reference":"owner/repository@v1","state":"invalid","host":"github.com","repository":"owner/repository","repository_id":42,"commit":"0123456789abcdef0123456789abcdef01234567","manifest":"action.yml","selected_object":"0123456789abcdef0123456789abcdef01234567","problems":[{"message":"bad","path":"action.yml","start":{"line":-1,"column":0},"end":{"line":0,"column":0}}]}`,
	} {
		data := []byte(resultPrefix + malformedRecord + resultSuffix)
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err == nil {
			t.Fatalf("schema accepted malformed action metadata: %s", malformedRecord)
		}
	}
}
