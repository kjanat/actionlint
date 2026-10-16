package fix

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/sourceid"
)

func TestPlanAndApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yml")
	before := "αβ\r\nvalue\n"
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	d := diagnostic.Diagnostic{Fixes: []diagnostic.Fix{{Applicability: "safe", Revisions: map[string]string{path: sourceid.Digest([]byte(before))}, Edits: []diagnostic.Edit{{Path: path, Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 2, Column: 1}, Replacement: "z\n"}}}}}
	reader := fsread.New(t.Context(), nil, nil)
	plan, err := Build(t.Context(), []diagnostic.Diagnostic{d}, reader, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].After != "αz\nvalue\n" {
		t.Fatalf("plan=%+v", plan)
	}
	paths, err := Apply(t.Context(), plan, reader)
	if err != nil || len(paths) != 1 {
		t.Fatalf("apply=%v %v", paths, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != plan.Changes[0].After {
		t.Fatalf("written=%q %v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if _, err := Apply(t.Context(), plan, reader); err == nil {
		t.Fatal("accepted stale plan")
	}
}

func TestRefuseInvalidTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.yml")
	content := []byte("hello\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	base := diagnostic.Edit{Path: path, Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 4}, Replacement: "ok"}
	for _, tc := range []struct {
		name, revision string
		edits          []diagnostic.Edit
	}{
		{"missing revision", "", []diagnostic.Edit{base}},
		{"stale revision", "bad", []diagnostic.Edit{base}},
		{"overlap", sourceid.Digest(content), []diagnostic.Edit{base, {Path: path, Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 5}}}},
		{"outside", sourceid.Digest(content), []diagnostic.Edit{{Path: path, Start: diagnostic.Position{Line: 9, Column: 1}, End: diagnostic.Position{Line: 9, Column: 2}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(t.Context(), []diagnostic.Diagnostic{{Fixes: []diagnostic.Fix{{Applicability: "safe", Revisions: map[string]string{path: tc.revision}, Edits: tc.edits}}}}, fsread.New(t.Context(), nil, nil), false)
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

func TestApplyPreflightsAllFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan := Plan{Changes: []Change{{a, "before", "after", sourceid.Digest([]byte("before"))}, {b, "stale", "after", sourceid.Digest([]byte("stale"))}}}
	if _, err := Apply(t.Context(), plan, fsread.New(t.Context(), nil, nil)); err == nil {
		t.Fatal("accepted stale second file")
	}
	got, _ := os.ReadFile(a)
	if string(got) != "before" {
		t.Fatal("wrote first file before validation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Apply(ctx, plan, fsread.New(ctx, nil, nil)); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestYAMLPhysicalCoordinates(t *testing.T) {
	for _, separator := range []string{"\r", "\r\n", "\n", "\u0085", "\u2028", "\u2029"} {
		content := []byte("\ufeffα" + separator + "β")
		offset, err := Offset(content, diagnostic.Position{Line: 2, Column: 1})
		if err != nil || string(content[offset:]) != "β" {
			t.Fatalf("separator=%q offset=%d err=%v", separator, offset, err)
		}
	}
}
