package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/ruff"
	"github.com/google/go-cmp/cmp"
)

func TestReleaseMatchesActionManifest(t *testing.T) {
	data, err := os.ReadFile("../../packages/github-action/tools/ruff.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		TagName string `json:"tagName"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TagName != release {
		t.Fatalf("Ruff selector metadata uses %s, Action uses %s; update the pinned metadata and regenerate", release, manifest.TagName)
	}
}

func TestVendoredSchema(t *testing.T) {
	t.Chdir("../..")
	if ruff.SchemaPath != schemaPath || ruff.SelectorSchemaPath != selectorSchemaPath {
		t.Fatal("generated schema paths are stale")
	}
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := targetVersions(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(versions, ruff.SupportedTargetVersions()) {
		t.Fatal("generated Python versions differ from the upstream schema")
	}
	wrapper, err := selectorSchema(schema, ruff.SupportedRuleSelectors())
	if err != nil {
		t.Fatal(err)
	}
	origin, err := provenance(schema)
	if err != nil {
		t.Fatal(err)
	}
	for filename, generated := range map[string][]byte{selectorSchemaPath: wrapper, provenancePath: origin} {
		stored, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		if err := json.Unmarshal(stored, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(generated, &want); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("%s is stale or upstream bytes changed (-generated +stored):\n%s", filename, diff)
		}
	}
}

func TestTargetVersions(t *testing.T) {
	for _, input := range []string{`{`, `{}`, `{"definitions":{"PythonVersion":{"enum":["py37"]}}}`} {
		if _, err := targetVersions([]byte(input)); err == nil {
			t.Fatalf("changed PythonVersion shape must fail generation: %s", input)
		}
	}
}

func TestSelectorSchemaDelta(t *testing.T) {
	input := []byte(`{"definitions":{"RuleSelector":{"enum":["ALL","F821","E111","undefined-name"]}}}`)
	got, err := selectorSchema(input, []string{"ALL", "F821", "PGH001"})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		AnyOf []struct {
			AllOf []struct {
				Ref     string `json:"$ref"`
				Pattern string `json:"pattern"`
				Not     struct {
					Enum []string `json:"enum"`
				} `json:"not"`
			} `json:"allOf"`
			Enum []string `json:"enum"`
		} `json:"anyOf"`
	}
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.AnyOf) != 2 || len(document.AnyOf[0].AllOf) != 3 {
		t.Fatal("expected upstream constraints and a separate compatibility branch")
	}
	constraints := document.AnyOf[0].AllOf
	if constraints[0].Ref != release+".schema.json#/definitions/RuleSelector" || constraints[1].Pattern != codePattern.String() ||
		!slices.Equal(constraints[2].Not.Enum, []string{"E111"}) || !slices.Equal(document.AnyOf[1].Enum, []string{"PGH001"}) {
		t.Fatalf("wrapper must contain only exclusions and missing aliases: %s", got)
	}
}

func TestSelectors(t *testing.T) {
	schema := []byte(`{"definitions":{"RuleSelector":{"enum":["ALL","ANN101","C90","C901","CPY","CPY001","F","F821","E","E111","E501","RUF","RUF001","RUF055","undefined-name","correctness"]}}}`)
	redirects := []byte(`("C9", "C90"), ("OLD", "F821"), ("PGH001", "F821"), ("PREVIEW", "RUF055"), ("REMOVED", "ANN101"), ("RUF940", "RUF950")`)
	rules := []byte(`[{"code":"C901","preview":false},{"code":"CPY001","preview":false},{"code":"F821","preview":false},{"code":"E111","preview":true},{"code":"E501","preview":false},{"code":"RUF001","preview":false},{"code":"RUF055","preview":true},{"code":"ANN101","preview":false,"status":{"Removed":{}}},{"code":"PGH001","preview":false,"status":{"Removed":{}}}]`)
	got, err := selectors(schema, redirects, rules)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ALL", "C9", "C90", "C901", "CPY", "CPY001", "E", "E501", "F", "F821", "OLD", "PGH001", "RUF", "RUF001"}
	if !slices.Equal(got, want) {
		t.Fatalf("selectors = %v, want %v", got, want)
	}
	for _, tc := range []struct{ schema, redirects, rules []byte }{
		{[]byte(`{`), redirects, rules},
		{[]byte(`{}`), redirects, rules},
		{schema, []byte(`unrecognized source`), rules},
		{schema, redirects, []byte(`{`)},
		{schema, redirects, []byte(`[]`)},
		{schema, redirects, []byte(`[{"code":"F821"}]`)},
	} {
		if _, err := selectors(tc.schema, tc.redirects, tc.rules); err == nil {
			t.Fatal("changed metadata shape must fail generation")
		}
	}
}
