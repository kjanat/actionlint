package output

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/edits"
)

func TestApplyRejectsTwoAliasesForOneFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path, alias := filepath.Join(root, "workflow.yml"), filepath.Join(root, "alias.yml")
	const before = "before"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	plan := edits.Plan{Changes: []edits.Change{
		{Path: path, Before: before, After: "first", Revision: sourceid.Digest([]byte(before))},
		{Path: alias, Before: before, After: "second", Revision: sourceid.Digest([]byte(before))},
	}}
	written, err := ApplyEdits(t.Context(), plan, files.New(t.Context(), nil, nil))
	if err == nil || !strings.Contains(err.Error(), "multiple fix plans") || len(written) != 0 {
		t.Fatalf("conflicting alias edits: written=%v error=%v", written, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != before || plan.Changes[1].Path != alias {
		t.Fatalf("rejected edits changed the file or plan: data=%q error=%v plan=%+v", data, err, plan)
	}
}

func TestPlanAndApply(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.yml")
	before := "αβ\r\nvalue\n"
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	d := results.Diagnostic{Fixes: []results.Fix{{Applicability: results.FixSafe, Revisions: map[string]string{path: sourceid.Digest([]byte(before))}, Edits: []results.Edit{{Path: path, Start: sourcepos.Position{Line: 1, Column: 2}, End: sourcepos.Position{Line: 2, Column: 1}, Replacement: "z\n"}}}}}
	reader := files.New(t.Context(), nil, nil)
	plan, err := edits.Build(t.Context(), []results.Diagnostic{d}, reader, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].After != "αz\nvalue\n" {
		t.Fatalf("plan=%+v", plan)
	}
	paths, err := ApplyEdits(t.Context(), plan, reader)
	if err != nil || len(paths) != 1 {
		t.Fatalf("apply=%v %v", paths, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != plan.Changes[0].After {
		t.Fatalf("written=%q %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != original.Mode().Perm() {
		t.Fatal(info.Mode())
	}
	if _, err := ApplyEdits(t.Context(), plan, reader); err == nil {
		t.Fatal("accepted stale plan")
	}
}

func TestApplyPreflightsAllFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan := edits.Plan{Changes: []edits.Change{
		{Path: a, Before: "before", After: "after", Revision: sourceid.Digest([]byte("before"))},
		{Path: b, Before: "stale", After: "after", Revision: sourceid.Digest([]byte("stale"))},
	}}
	if _, err := ApplyEdits(t.Context(), plan, files.New(t.Context(), nil, nil)); err == nil {
		t.Fatal("accepted stale second file")
	}
	got, _ := os.ReadFile(a)
	if string(got) != "before" {
		t.Fatal("wrote first file before validation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ApplyEdits(ctx, plan, files.New(ctx, nil, nil)); err == nil {
		t.Fatal("ignored cancellation")
	}
}
