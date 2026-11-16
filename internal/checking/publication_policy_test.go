package checking

import (
	"errors"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestFinalPublicationCannotAuditConflictedSource(t *testing.T) {
	t.Parallel()
	for _, conflicted := range []bool{false, true} {
		t.Run(map[bool]string{false: "captured", true: "conflicted"}[conflicted], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			store, fixture := capturefixture.NewCapturedSources(t, root)
			entryPath, foreignPath := filepath.Join(root, "ci.yml"), filepath.Join(root, "action.yml")
			entry, err := fixture.Capture(entryPath, []byte(commandGoodWorkflow), sourcetext.YAML)
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("run: echo ok # actionlint-ignore shellcheck -- unused\n")
			foreign, err := fixture.Capture(foreignPath, content, sourcetext.YAML)
			if err != nil {
				t.Fatal(err)
			}
			rules := ConfigurationRules()
			registry := rulebinding.NewRegistry(rules)
			scope := registry.Prepare(nil, nil, nil)
			batch := &policy.Batch{
				Processor:  policy.Processor{RuleSet: rules},
				Policy:     policy.Policy{Path: entryPath, ConfigPath: entryPath, WorkingDir: root, Content: entry.Bytes(), Scope: scope, Bindings: registry.Seal(), SourceBindings: map[string]*rulebinding.Scope{foreignPath: scope}},
				Sources:    map[string][]byte{foreignPath: content},
				References: map[string]facts.SourceIndex{foreignPath: {Document: sourcetext.Reference{Document: foreign.Revision().Document(), Revision: foreign.Revision().Digest()}}},
			}
			var conflict error
			if conflicted {
				conflict = store.Quarantine(foreign, "contradictory source model")
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil {
				t.Fatal(err)
			}
			result := analysisResult{files: []analyzedFile{{captured: entry, source: workspace.InputFile{Path: entryPath, InputPath: entryPath, Content: entry.Bytes()}, publication: batch}}}
			validationErr := result.revalidateCaptures(store, rows, root, nil)
			if conflicted && !errors.Is(validationErr, conflict) {
				t.Fatalf("lost source conflict: %v", validationErr)
			}
			if err := result.publishDiagnostics(); err != nil {
				t.Fatal(err)
			}
			if conflicted {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("conflicted source produced policy diagnostics: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "unused-suppression" {
				t.Fatalf("healthy source lost its suppression audit: %+v", result.Diagnostics)
			}
		})
	}
}
