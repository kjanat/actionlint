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
	content := []byte("hello\n")
	base := func(path string) results.Edit {
		return results.Edit{Path: path, Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 4}, Replacement: "ok"}
	}
	for _, tc := range []struct {
		name, revision string
		edits          func(path string) []results.Edit
	}{
		{"empty alternative", sourceid.Digest(content), func(string) []results.Edit { return nil }},
		{"missing revision", "", func(path string) []results.Edit { return []results.Edit{base(path)} }},
		{"stale revision", "bad", func(path string) []results.Edit { return []results.Edit{base(path)} }},
		{"overlap", sourceid.Digest(content), func(path string) []results.Edit {
			return []results.Edit{base(path), {Path: path, Start: sourcepos.Position{Line: 1, Column: 2}, End: sourcepos.Position{Line: 1, Column: 5}}}
		}},
		{"outside", sourceid.Digest(content), func(path string) []results.Edit {
			return []results.Edit{{Path: path, Start: sourcepos.Position{Line: 9, Column: 1}, End: sourcepos.Position{Line: 9, Column: 2}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "source.yml")
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Build(t.Context(), []results.Diagnostic{{Fixes: []results.Fix{{Applicability: results.FixSafe, Revisions: map[string]string{path: tc.revision}, Edits: tc.edits(path)}}}}, files.New(t.Context(), nil, nil), false)
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
