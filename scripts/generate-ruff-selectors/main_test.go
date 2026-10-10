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
	schema := []byte(`{"definitions":{"RuleSelector":{"enum":["ALL","C90","C901","F","F821","E111","undefined-name","correctness"]}}}`)
	redirects := []byte(`("C9", "C90"), ("OLD", "F821"), ("REMOVED", "F999"), ("RUF940", "RUF950")`)
	got, err := selectors(schema, redirects)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ALL", "C9", "C90", "C901", "E111", "F", "F821", "OLD"}
	if !slices.Equal(got, want) {
		t.Fatalf("selectors = %v, want %v", got, want)
	}
	for _, tc := range []struct{ schema, redirects []byte }{
		{[]byte(`{`), redirects},
		{[]byte(`{}`), redirects},
		{schema, []byte(`unrecognized source`)},
	} {
		if _, err := selectors(tc.schema, tc.redirects); err == nil {
			t.Fatal("changed metadata shape must fail generation")
		}
	}
}
