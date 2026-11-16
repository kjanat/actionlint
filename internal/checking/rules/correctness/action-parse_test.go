package correctness

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/source"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/actions"
	"actionlint.kjanat.dev/v2/internal/input/files"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestMetadataParseFindingCarriesItsLookupRevision(t *testing.T) {
	t.Parallel()
	for _, mapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "cache", true: "mapped resolver"}[mapped], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			content := []byte("name: [\n")
			var reads atomic.Int32
			reader := files.New(t.Context(), func(string) ([]byte, error) {
				if reads.Add(1) != 1 {
					return []byte("name: changed\nruns: {using: composite, steps: []}\n"), nil
				}
				return content, nil
			}, nil)
			cache := actions.NewCache(t, root, nil, reader)
			var lookup workspace.ActionMetadataDocumentLookup = cache
			spec := "./broken"
			if mapped {
				lookup = workspace.NewResolver(cache, map[string]string{"./generated": "./broken"})
				spec = "./generated"
			}
			rule := NewRuleAction(t.Context(), ActionRule.Base(), lookup, nil)
			step := &definition.Step{Exec: &definition.ExecAction{Uses: &definition.String{Value: spec, Pos: &source.Position{Line: 3, Column: 5}}}}
			if err := rule.VisitStep(declarations.Step(step)); err != nil {
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

func (lookup metadataParseFailureLookup) FindMetadataDocument(string) (*actionmeta.Metadata, bool, workspace.SourceDocument, error) {
	return nil, false, workspace.SourceDocument{}, workspace.NewParseError("broken/action.yml", lookup.cause)
}

func TestMetadataParseFindingRejectsMissingCaptureAndInterruption(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{errors.New("invalid YAML"), context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			rule := NewRuleAction(t.Context(), ActionRule.Base(), metadataParseFailureLookup{cause: failure}, nil)
			err := rule.VisitStep(declarations.Step(&definition.Step{Exec: &definition.ExecAction{Uses: &definition.String{Value: "./broken", Pos: &source.Position{Line: 1, Column: 1}}}}))
			if !errors.Is(err, failure) || len(rule.Errs()) != 0 {
				t.Fatalf("unbound or interrupted parse became a finding: error=%v findings=%+v", err, rule.Errs())
			}
		})
	}
}
