package actionlint

import (
	"strings"
	"testing"
)

func TestPopularActionsDataset(t *testing.T) {
	if len(PopularActions) == 0 {
		t.Fatal("popular actions data set is empty")
	}

	for n, meta := range PopularActions {
		if meta == nil {
			t.Fatalf("metadata for %s is nil", n)
		}
		if meta.Name == "" {
			t.Fatalf("action name for %s is empty", n)
		}
		for id, i := range meta.Inputs {
			if id != strings.ToLower(id) {
				t.Errorf("input ID %q is not in lower case at %q", id, n)
			}
			if i.Name == "" {
				t.Errorf("input name is not empty at ID %q at %q", id, n)
			}
		}
		for id, o := range meta.Outputs {
			if id != strings.ToLower(id) {
				t.Errorf("output ID %q is not in lower case at %q", id, n)
			}
			if o.Name == "" {
				t.Errorf("output name is not empty at ID %q at %q", id, n)
			}
		}
	}
}

func TestPopularActionsCurrentMajors(t *testing.T) {
	for _, spec := range []string{
		"actions/cache@v6",
		"actions/checkout@v7",
		"actions/setup-go@v7",
		"codecov/codecov-action@v7",
		"dawidd6/action-download-artifact@v23",
	} {
		if _, ok := PopularActions[spec]; !ok {
			t.Errorf("popular action %q is missing", spec)
		}
	}
}
