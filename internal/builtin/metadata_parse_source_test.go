package builtin

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestMetadataParseFindingCarriesItsLookupRevision(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "cache", true: "mapped resolver"}[mapped], func(t *testing.T) {
			root := t.TempDir()
			content := []byte("name: [\n")
			var reads atomic.Int32
			reader := fsread.New(t.Context(), func(string) ([]byte, error) {
				if reads.Add(1) != 1 {
					return []byte("name: changed\nruns: {using: composite, steps: []}\n"), nil
				}
				return content, nil
			}, nil)
			cache := newCapturedLocalCache(t, root, nil, reader)
			var lookup actionlocal.MetadataDocumentLookup = cache
			spec := "./broken"
			if mapped {
				lookup = actionlocal.NewResolver(cache, map[string]string{"./generated": "./broken"})
				spec = "./generated"
			}
			rule := NewRuleAction(lookup)
			step := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: &workflowlang.Pos{Line: 3, Col: 5}}}}
			if err := rule.VisitStep(ownedStepFixture(step)); err != nil {
				t.Fatal(err)
			}
			result, cached := cache.LookupMetadata("./broken")
			document, exists := cache.SourceDocument(result)
			findings := rule.Errs()
			if !cached || !exists || len(findings) != 1 || !findings[0].MetadataParseSource.Valid() || findings[0].MetadataParseSource != document.Capture().Revision() {
				t.Fatalf("finding was not bound at creation: findings=%+v source=%+v", findings, document)
			}
			if findings[0].Start.Line != 3 || findings[0].Start.Column != 5 || document.Address() != filepath.Join(root, "broken", "action.yml") || string(document.Content()) != string(content) || reads.Load() != 1 {
				t.Fatalf("parse finding lost its caller or reread source: finding=%+v reads=%d", findings[0], reads.Load())
			}
		})
	}
}

type metadataParseFailureLookup struct{ cause error }

func (lookup metadataParseFailureLookup) FindMetadataDocument(string) (*actionmeta.Metadata, bool, actionlocal.SourceDocument, error) {
	return nil, false, actionlocal.SourceDocument{}, actionlocal.NewParseError("broken/action.yml", lookup.cause)
}

func TestMetadataParseFindingRejectsMissingCaptureAndInterruption(t *testing.T) {
	for _, failure := range []error{errors.New("invalid YAML"), context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			rule := NewRuleAction(metadataParseFailureLookup{cause: failure})
			err := rule.VisitStep(ownedStepFixture(&workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./broken", Pos: &workflowlang.Pos{Line: 1, Col: 1}}}}))
			if !errors.Is(err, failure) || len(rule.Errs()) != 0 {
				t.Fatalf("unbound or interrupted parse became a finding: error=%v findings=%+v", err, rule.Errs())
			}
		})
	}
}
