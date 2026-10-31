package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestActionValidationPropagatesMetadataInterruption(t *testing.T) {
	for _, spec := range []string{"./inner", "$/inner"} {
		for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(spec+"/"+failure.Error(), func(t *testing.T) {
				reader := fsread.New(t.Context(), func(string) ([]byte, error) {
					return nil, failure
				}, nil)
				cache := newCapturedLocalCache(t, t.TempDir(), nil, reader)
				rule := NewRuleAction(cache)
				err := rule.VisitStep(ownedStepFixture(&workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: &workflowlang.Pos{Line: 1, Col: 1}}}}))
				if !errors.Is(err, failure) || len(rule.Errs()) != 0 {
					t.Fatalf("interruption became a lint finding: error=%v findings=%+v", err, rule.Errs())
				}
			})
		}
	}
}

func TestMetadataIdentityUsesCapturedCanonicalDocumentAfterRetarget(t *testing.T) {
	root := t.TempDir()
	first, second, alias := filepath.Join(root, "first", "action.yml"), filepath.Join(root, "second", "action.yml"), filepath.Join(root, "alias.yml")
	content := []byte("name: source\ndescription: source\nruns: {using: node24, main: index.js}\n")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, alias); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	registry := declarations.NewCapturedSnapshots(t, root)
	snapshot, err := registry.Document(alias, content, documentparse.Action(alias, alias, content))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := actionmeta.Parse(alias, content)
	if err != nil {
		t.Fatal(err)
	}
	finding := &findingmodel.Finding{Message: "manifest " + strconv.Quote(metadata.Path()) + " in " + strconv.Quote(metadata.Dir())}
	document := snapshot.Reference().Document
	before := metadataFindingKey(metadata, document, finding)
	if err := registry.Store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	after := metadataFindingKey(metadata, document, finding)
	if before != after || after.Path != document || !strings.Contains(after.Message, strconv.Quote(document)) {
		t.Fatalf("metadata key recanonicalized a sealed alias: before=%+v after=%+v", before, after)
	}
}

func TestStandaloneMetadataRequiresMatchingCapturedSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.js"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "action.yml")
	content := []byte("name: source\nruns: {using: node24, main: index.js}\n")
	registry := declarations.NewCapturedSnapshots(t, root)
	snapshot, err := registry.Document(path, content, documentparse.Action(path, path, content))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := actionmeta.Parse(path, content)
	if err != nil {
		t.Fatal(err)
	}
	rule := NewRuleAction(nil)
	if err := rule.CheckStandaloneMetadata(metadata, documentparse.SourceSnapshot{}); err == nil {
		t.Fatal("missing source was accepted")
	}
	other, err := actionmeta.Parse(path, append([]byte("# another revision\n"), content...))
	if err != nil {
		t.Fatal(err)
	}
	if err := rule.CheckStandaloneMetadata(other, snapshot); err == nil || len(rule.Errs()) != 0 {
		t.Fatalf("mismatching parse source was accepted: %v", err)
	}
	if err := rule.CheckStandaloneMetadata(metadata, snapshot); err != nil {
		t.Fatal(err)
	}
	if len(rule.Errs()) != 1 || rule.Errs()[0].MetadataIdentity == nil || rule.Errs()[0].MetadataIdentity.Key.Path != snapshot.Reference().Document {
		t.Fatalf("standalone metadata lost captured identity: %+v", rule.Errs())
	}
}

func TestMetadataValidationDebugLog(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "inner/action.yml", "name: inner\nruns: {using: node24, main: index.js}\n")
	writeFixture(t, root, "inner/index.js", "")
	var cacheLog, ruleLog strings.Builder
	rule := newRuleAction(newCapturedLocalCache(t, root, &cacheLog))
	rule.EnableDebug(&ruleLog)
	for i := 1; i <= 2; i++ {
		if err := rule.checkLocalAction("./inner", "./inner", ownedActionFixture(&workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./inner", Pos: &workflowlang.Pos{Line: i, Col: 1}}})); err != nil {
			t.Fatal(err)
		}
	}
	if want := "[LocalActionsCache] New metadata parsed from action " + filepath.Join(root, "inner") + ": node24 action \"inner\"\n"; !strings.Contains(cacheLog.String(), want) {
		t.Fatalf("cache log lacks %q: %q", want, cacheLog.String())
	}
	if want := "[action] Checking metadata of node24 action \"inner\" at \"./inner\"\n"; ruleLog.String() != want {
		t.Fatalf("rule log = %q, want %q", ruleLog.String(), want)
	}
}

func TestMetadataValidationTemplatesDetached(t *testing.T) {
	root := t.TempDir()
	// The missing description is positioned at each caller; the composite step
	// without a shell is positioned in the metadata and carries an ignore origin.
	writeFixture(t, root, "inner/action.yml", "name: inner\nruns:\n  using: composite\n  steps:\n    - run: echo\n")
	cache := newCapturedLocalCache(t, root, nil)
	rule := newRuleAction(cache)
	for i := 1; i <= 3; i++ {
		action := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./inner", Pos: &workflowlang.Pos{Line: i, Col: 1}}}
		if err := rule.checkLocalAction("./inner", "./inner", ownedActionFixture(action)); err != nil {
			t.Fatal(err)
		}
	}
	templates := rule.validation.templates[filepath.Join(root, "inner", "action.yml")]
	if len(rule.validation.templates) != 1 || len(templates) != 2 || len(rule.Errs()) != 3*len(templates) {
		t.Fatalf("documents=%d templates=%d findings=%d", len(rule.validation.templates), len(templates), len(rule.Errs()))
	}
	described, originated := 0, 0
	for i, finding := range rule.Errs() {
		if strings.Contains(finding.Message, "description is required") {
			described++
			if finding.Start.Line != i/len(templates)+1 {
				t.Fatalf("description finding not at its caller: %+v", finding)
			}
		}
		if finding.IgnoreOrigin != nil {
			originated++
		}
	}
	if described != 3 || originated != 3 {
		t.Fatalf("description findings=%d origin findings=%d", described, originated)
	}
	// Annotation is the only write path into recorded findings; it must not
	// reach the cached templates the next visit replays.
	rule.AnnotateSince(0, func(finding *findingmodel.Finding) {
		finding.Message = "mutated caller"
		if finding.IgnoreOrigin != nil {
			finding.IgnoreOrigin.Job = "mutated caller"
		}
	})
	for _, template := range templates {
		if template.Message == "mutated caller" || template.IgnoreOrigin != nil && template.IgnoreOrigin.Job == "mutated caller" {
			t.Fatalf("annotation mutated cached template: %+v", template)
		}
	}
}
