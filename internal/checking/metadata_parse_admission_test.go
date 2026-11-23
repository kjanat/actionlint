package checking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results/report"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	"actionlint.kjanat.dev/v2/internal/checking/executor/composites"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/results/finding"
)

type metadataAdmissionAnalyzer struct{ inspect func() error }

func (metadataAdmissionAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "metadata-admission-probe", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Source}}
}

func (a metadataAdmissionAnalyzer) AnalyzeDocument(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	if a.inspect != nil {
		if err := a.inspect(); err != nil {
			return nil, err
		}
	}
	return []results.Diagnostic{{Message: "unrelated entry observation", Start: sourcetext.Position{Line: 1, Column: 1}}}, nil
}

func TestAnalyzeMetadataParseFindingSurvivesHealthyCaptureAndRejectsQuarantine(t *testing.T) {
	t.Parallel()
	for _, quarantined := range []bool{false, true} {
		t.Run(map[bool]string{false: "public Analyze healthy", true: "full run quarantined after composition"}[quarantined], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			metadataPath := filepath.Join(root, "broken", "action.yml")
			if err := os.MkdirAll(filepath.Dir(metadataPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(metadataPath, []byte("name: [\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			unit := workspace.InputFile{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/broken}]}}\npermissions: {}\n")}
			probe := metadataAdmissionAnalyzer{}
			var result *report.Analysis
			var err, conflict error
			if !quarantined {
				result, err = Analyze(t.Context(), Options{WorkingDir: root, Analyzers: []results.AnalyzerRegistration{{Analyzer: probe}}}, []workspace.InputFile{unit})
			} else {
				var store *capture.Store
				probe.inspect = func() error {
					captured, state, err := store.Lookup(metadataPath)
					if err != nil {
						return fmt.Errorf("lookup real manifest before analyzer: %w", err)
					}
					if state != capture.Captured {
						return fmt.Errorf("real manifest was not captured before analyzer: state=%s", state)
					}
					conflict = store.Quarantine(captured, "test observed contradictory manifest provenance")
					if conflict == nil {
						return errors.New("manifest was not quarantined")
					}
					return nil
				}
				processes := hostprocess.NewProcessPool(t.Context(), 1)
				defer processes.Wait()
				request := analysisRequest{WorkingDir: root, Sources: []workspace.InputFile{unit}, Checks: testChecks(t, []results.AnalyzerRegistration{{Analyzer: probe}}, nil), Processes: processes}
				completed, runErr := runWithSourceCaptureConstructor(t.Context(), request, io.Discard, LogLevelNone,
					func(ctx context.Context, overlays []capture.Overlay, reader capture.Reader) (*capture.Store, error) {
						var initErr error
						store, initErr = capture.New(ctx, overlays, reader)
						return store, initErr
					})
				err = runErr
				if completed != nil {
					result = &completed.Analysis
				}
			}
			if result == nil {
				t.Fatalf("analysis returned no result: %v", err)
			}
			parseCount, retainedCount := 0, 0
			for _, finding := range result.Diagnostics {
				if finding.Rule == "syntax-check" && strings.Contains(finding.Message, "could not parse action metadata") {
					parseCount++
				}
				if finding.Message == "unrelated entry observation" {
					retainedCount++
				}
			}
			if !quarantined {
				if err != nil || parseCount != 1 || retainedCount != 1 || len(result.Diagnostics) != 2 {
					t.Fatalf("healthy real manifest lost its parse diagnostic: diagnostics=%+v error=%v", result.Diagnostics, err)
				}
				return
			}
			rejection, ok := errors.AsType[*admissionRejectedError](err)
			if conflict == nil || !errors.Is(err, conflict) || !ok || rejection.Count() != 1 || parseCount != 0 || retainedCount != 1 || len(result.Diagnostics) != 1 {
				t.Fatalf("quarantined real manifest leaked or unrelated finding lost: diagnostics=%+v error=%v", result.Diagnostics, err)
			}
		})
	}
}

func TestMissingParseProvenanceReportsCanonicalResolutionFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, _ := capturefixture.NewCapturedSources(t, root)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	session := analysisSession{ctx: ctx, captures: store, workingDir: root}
	path := filepath.Join("broken", "action.yml")
	rejected, err := session.admitSourceDocuments(composites.Composition{Sources: map[string][]byte{path: []byte("name: [\n")}}, map[string]facts.SourceIndex{})
	if !errors.Is(err, context.Canceled) || !rejected[path] || !strings.Contains(err.Error(), "no admitted parse provenance") {
		t.Fatalf("canonical resolution failure was hidden: rejected=%v error=%v", rejected, err)
	}
	if _, state, err := store.Lookup(filepath.Join(root, path)); err != nil || state != capture.Absent {
		t.Fatalf("cancelled rejection resolution acquired a source: state=%s error=%v", state, err)
	}
}

func TestMetadataSourceAdmissionRejectsMissingProvenanceAndVerifyMismatch(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "Verify mismatch", true: "missing parse provenance"}[missing], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			entryPath, metadataPath := filepath.Join(root, "ci.yml"), filepath.Join(root, "broken", "action.yml")
			store, fixture := capturefixture.NewCapturedSources(t, root)
			entry, err := fixture.Capture(entryPath, []byte(commandGoodWorkflow), sourcetext.YAML)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := fixture.Capture(metadataPath, []byte("name: [\n"), sourcetext.YAML)
			if err != nil {
				t.Fatal(err)
			}
			cache := localactions.NewManifestCache(t.Context(), root, nil)
			cache.Configure(localactions.Options{Captures: store, Context: t.Context(), WorkingDir: root})
			lookup, _ := cache.LookupMetadata("./broken")
			document, exists := cache.SourceDocument(lookup)
			if !exists {
				t.Fatal("metadata parse record absent")
			}
			producer := correctness.NewRuleAction(t.Context(), correctness.InvalidActionReferenceRule.Base(), cache, nil)
			if err := producer.VisitStep(ownedApplicationStep(&definition.Step{Exec: &definition.ExecAction{Uses: &definition.String{Value: "./broken", Pos: &sourcetext.Position{Line: 1, Column: 1}}}})); err != nil || len(producer.Errs()) != 1 {
				t.Fatalf("real parse producer failed: %v", err)
			}
			parse := producer.Errs()[0]
			retained := &finding.Finding{Message: "entry observation"}
			displayPath := filepath.Join("broken", "action.yml")
			composed := composites.Composition{Sources: map[string][]byte{displayPath: []byte("different consumer bytes")}, SourceDocuments: map[string]composites.SourceDocument{displayPath: document}}
			if missing {
				composed.Sources[displayPath] = metadata.Bytes()
				delete(composed.SourceDocuments, displayPath)
			}
			registry, err := parsing.NewSourceSnapshots(store)
			if err != nil {
				t.Fatal(err)
			}
			session := analysisSession{ctx: t.Context(), captures: store, snapshots: registry, workingDir: root}
			references := map[string]facts.SourceIndex{}
			rejected, sourceErr := session.admitSourceDocuments(composed, references)
			if sourceErr == nil || !rejected[displayPath] || !rejected[metadata.Revision().Document()] || len(references) != 0 {
				t.Fatalf("bad source retained canonical admission: rejected=%v references=%v error=%v", rejected, references, sourceErr)
			}
			if err := store.VerifyHandle(metadata); err != nil {
				t.Fatalf("consumer failure quarantined the shared healthy capture: %v", err)
			}
			if !missing {
				if _, ok := errors.AsType[capture.ConflictError](sourceErr); !ok {
					t.Fatalf("Verify mismatch lost typed cause: %v", sourceErr)
				}
			}
			accepted, admissionErr := session.admitReportedSources([]*finding.Finding{parse, retained}, "ci.yml", entryPath, composed, rejected)
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil {
				t.Fatal(err)
			}
			result := analysisResult{files: []analyzedFile{{captured: entry, source: workspace.InputFile{Path: "ci.yml", InputPath: entryPath, Content: entry.Bytes()}, errors: accepted}}}
			err = result.revalidateCaptures(store, rows, root, errors.Join(sourceErr, admissionErr))
			publishTestDiagnostics(t, &result)
			rejection, ok := errors.AsType[*admissionRejectedError](err)
			if !ok || rejection.Count() != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != retained.Message {
				t.Fatalf("failed source leaked its real parse finding: diagnostics=%+v error=%v", result.Diagnostics, err)
			}
		})
	}
}
