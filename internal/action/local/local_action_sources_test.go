package local

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func TestLocalActionSourceMappings(t *testing.T) {
	root := t.TempDir()
	first := writeFixture(t, root, "source/action.yml", "name: Source\ndescription: Source\ninputs:\n  token:\n    description: Token\n    required: true\noutputs:\n  result:\n    description: Result\nruns:\n  using: composite\n  steps:\n    - run: echo test\n      shell: bash\n")
	writeFixture(t, root, "other/action.yml", "name: Other\ndescription: Other\nruns: {using: composite, steps: []}\n")
	base := NewCache(root, nil)
	view := &Resolver{base: base, sourceMappings: map[string]string{"./generated": "./source"}, usedSources: map[string][]byte{}}
	for _, spec := range []string{"./generated", "$/generated", "./generated/../generated"} {
		meta, _, err := view.FindMetadata(spec)
		if err != nil || meta == nil {
			t.Fatalf("%s: %v, %v", spec, meta, err)
		}
		if meta.Path() != first || !meta.SourceMapped || meta.Inputs["token"] == nil || meta.Outputs["result"] == nil {
			t.Fatalf("wrong mapped metadata: %+v", meta)
		}
		if _, ok := view.usedSources[first]; !ok {
			t.Fatal("source manifest not recorded")
		}
	}
	second := &Resolver{base: base, sourceMappings: map[string]string{"./generated": "./other"}}
	meta, _, err := second.FindMetadata("./generated")
	if err != nil || meta.Name != "Other" {
		t.Fatalf("mapping leaked between views: %+v %v", meta, err)
	}
	plain, _, err := base.FindMetadata("./source")
	if err != nil || plain.SourceMapped {
		t.Fatalf("base metadata changed: %+v %v", plain, err)
	}
	if _, _, err := base.FindMetadata("./generated"); err == nil {
		t.Fatal("mapping leaked into base")
	}
	if _, _, err := view.FindMetadata("./generated/nested"); err == nil {
		t.Fatal("mapping unexpectedly matched a child directory")
	}
	view.setCheckout(execution.Directory{Kind: execution.DirectoryKnown, Path: "checkout"})
	if _, _, err := view.FindMetadata("./checkout/generated"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := view.FindMetadata("$/generated"); err != nil {
		t.Fatal(err)
	}
	view.restoreCheckout(&execution.CheckoutPlacement{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: "."}, Foreign: true})
	if meta, _, err := view.FindMetadata("./generated"); err != nil || meta != nil {
		t.Fatalf("mapping resolved a foreign checkout: %+v %v", meta, err)
	}
}

func TestLocalActionSourceSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeFixture(t, outside, "action.yml", "name: Outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "source")); err != nil {
		t.Skip(err)
	}
	view := &Resolver{base: NewCache(root, nil), sourceMappings: map[string]string{"./generated": "./source"}}
	if _, _, err := view.FindMetadata("./generated"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("got %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "action.yml"), filepath.Join(root, "manifest", "action.yml")); err != nil {
		t.Fatal(err)
	}
	view.sourceMappings["./generated"] = "./manifest"
	if _, _, err := view.FindMetadata("./generated"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("got %v", err)
	}
}

func TestLocalActionSourceCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "Source/action.yml", "name: Source\ndescription: Source\nruns: {using: composite, steps: []}\n")
	view := &Resolver{base: NewCache(root, nil), sourceMappings: map[string]string{"./generated": "./source"}, caseInsensitive: true}
	if meta, _, err := view.FindMetadata("./GENERATED"); err != nil || meta == nil {
		t.Fatalf("%+v %v", meta, err)
	}
	view.sourceMappings["./Generated"] = "./Source"
	if _, _, err := view.FindMetadata("./GENERATED"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("got %v", err)
	}
}

func TestLocalActionUnknownCheckoutMissingMetadata(t *testing.T) {
	for _, folded := range []bool{false, true} {
		root := t.TempDir()
		writeFixture(t, root, "present/action.yml", "name: present\ndescription: present\nruns: {using: composite, steps: []}\n")
		view := &Resolver{base: NewCache(root, nil), caseInsensitive: folded}
		view.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
		for range 2 {
			if meta, _, err := view.FindMetadata("./private/action"); err != nil || meta != nil {
				t.Fatalf("unknown checkout: %+v %v", meta, err)
			}
		}
		if meta, _, err := view.FindMetadata("./present"); err != nil || meta == nil {
			t.Fatalf("lost best-effort metadata: %+v %v", meta, err)
		}
		if _, _, err := view.FindMetadata("$/private/action"); err == nil {
			t.Fatal("self repository missing metadata must be reported")
		}
		view.sourceMappings = map[string]string{"./private/action": "./absent-source"}
		if _, _, err := view.FindMetadata("./private/action"); err == nil {
			t.Fatal("explicit missing source must be reported")
		}
		view.sourceMappings = nil
		view.setCheckout(execution.Directory{Kind: execution.DirectoryKnown})
		if _, _, err := view.FindMetadata("./private/action"); err == nil {
			t.Fatal("known checkout missing metadata must be reported")
		}
	}
}

func TestMissingLocalActionMappedSourceDiagnostic(t *testing.T) {
	for _, folded := range []bool{false, true} {
		root := t.TempDir()
		view := &Resolver{base: NewCache(root, nil), sourceMappings: map[string]string{"./generated": "./absent-source"}, caseInsensitive: folded}
		_, _, err := view.FindMetadata("./generated")
		if _, missing := errors.AsType[UnresolvedError](err); !missing {
			t.Fatalf("folded=%v: expected missing-local-action: %v", folded, err)
		}
		want := `source mapping for local action "./generated": local action "./absent-source" has no action.yml, action.yaml, Dockerfile or dockerfile; check the path and ensure the action is available when linting`
		if err.Error() != want {
			t.Fatalf("folded=%v: got %q; want %q", folded, err, want)
		}
	}
}
