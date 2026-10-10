package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
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
