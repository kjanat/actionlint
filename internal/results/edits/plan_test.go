package edits

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRefuseInvalidTransactions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "source.yml")
	content := []byte("hello\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	base := results.Edit{Path: path, Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 4}, Replacement: "ok"}
	for _, tc := range []struct {
		name, revision string
		edits          []results.Edit
	}{
		{"empty alternative", sourceid.Digest(content), nil},
		{"missing revision", "", []results.Edit{base}},
		{"stale revision", "bad", []results.Edit{base}},
		{"overlap", sourceid.Digest(content), []results.Edit{base, {Path: path, Start: sourcepos.Position{Line: 1, Column: 2}, End: sourcepos.Position{Line: 1, Column: 5}}}},
		{"outside", sourceid.Digest(content), []results.Edit{{Path: path, Start: sourcepos.Position{Line: 9, Column: 1}, End: sourcepos.Position{Line: 9, Column: 2}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Build(t.Context(), []results.Diagnostic{{Fixes: []results.Fix{{Applicability: results.FixSafe, Revisions: map[string]string{path: tc.revision}, Edits: tc.edits}}}}, files.New(t.Context(), nil, nil), false)
			if err == nil {
				t.Fatal("accepted invalid edits")
			}
			got, _ := os.ReadFile(path)
			if string(got) != string(content) {
				t.Fatal("modified source")
			}
		})
	}
}

func TestYAMLPhysicalCoordinates(t *testing.T) {
	t.Parallel()
	for _, separator := range []string{"\r", "\r\n", "\n", "\u0085", "\u2028", "\u2029"} {
		content := []byte("\ufeffα" + separator + "β")
		offset, err := Offset(content, sourcepos.Position{Line: 2, Column: 1})
		if err != nil || string(content[offset:]) != "β" {
			t.Fatalf("separator=%q offset=%d err=%v", separator, offset, err)
		}
	}
}
