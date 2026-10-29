package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/source"
)

type metadataAdmissionAnalyzer struct{ inspect func() error }

func (metadataAdmissionAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "metadata-admission-probe", Requires: []analyzer.Fact{analyzer.Source}}
}

func (a metadataAdmissionAnalyzer) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if a.inspect != nil {
		if err := a.inspect(); err != nil {
			return nil, err
		}
	}
	return []diagnostic.Diagnostic{{Message: "unrelated entry observation", Start: diagnostic.Position{Line: 1, Column: 1}}}, nil
}

func TestAnalyzeMetadataParseFindingSurvivesHealthyCaptureAndRejectsQuarantine(t *testing.T) {
	for _, quarantined := range []bool{false, true} {
		t.Run(map[bool]string{false: "public Analyze healthy", true: "full run quarantined after composition"}[quarantined], func(t *testing.T) {
			root := t.TempDir()
			metadataPath := filepath.Join(root, "broken", "action.yml")
			if err := os.MkdirAll(filepath.Dir(metadataPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(metadataPath, []byte("name: [\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			unit := sourceUnit{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/broken}]}}\n")}
			probe := metadataAdmissionAnalyzer{}
			var result *Result
			var err, conflict error
			if !quarantined {
				result, err = Analyze(t.Context(), Options{WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: probe}}}, []sourceUnit{unit})
			} else {
				// The existing constructor seam gives the test the real result owner;
				// the analyzer runs after composition and source verification.
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
				processes := analyzerhost.NewProcessPool(t.Context(), 1)
				defer processes.Wait()
				request := analysisRequest{WorkingDir: root, Sources: []sourceUnit{unit}, Analyzers: []analyzer.Registration{{Analyzer: probe}}, Processes: processes}
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
				if finding.Rule == "action" && strings.Contains(finding.Message, "could not parse action metadata") {
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
	root := t.TempDir()
	fixture := capturefixture.NewCapturedSources(t, root)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	session := analysisSession{ctx: ctx, captures: fixture.Store, workingDir: root}
	path := filepath.Join("broken", "action.yml")
	rejected, err := session.admitSourceDocuments(composition.Result{Sources: map[string][]byte{path: []byte("name: [\n")}}, map[string]workflowfacts.SourceIndex{})
	if !errors.Is(err, context.Canceled) || !rejected[path] || !strings.Contains(err.Error(), "no admitted parse provenance") {
		t.Fatalf("canonical resolution failure was hidden: rejected=%v error=%v", rejected, err)
	}
	if _, state, err := fixture.Store.Lookup(filepath.Join(root, path)); err != nil || state != capture.Absent {
		t.Fatalf("cancelled rejection resolution acquired a source: state=%s error=%v", state, err)
	}
}

func TestMetadataSourceAdmissionRejectsMissingProvenanceAndVerifyMismatch(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "Verify mismatch", true: "missing parse provenance"}[missing], func(t *testing.T) {
			root := t.TempDir()
			entryPath, metadataPath := filepath.Join(root, "ci.yml"), filepath.Join(root, "broken", "action.yml")
			fixture := capturefixture.NewCapturedSources(t, root)
			entry, err := fixture.Capture(entryPath, []byte(commandGoodWorkflow), source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := fixture.Capture(metadataPath, []byte("name: [\n"), source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			cache := actionlocal.NewCache(t.Context(), root, nil)
			cache.Configure(actionlocal.Options{Captures: fixture.Store, Context: t.Context(), WorkingDir: root})
			lookup, _ := cache.LookupMetadata("./broken")
			document, exists := cache.SourceDocument(lookup)
			if !exists {
				t.Fatal("metadata parse record absent")
			}
			producer := builtin.NewRuleAction(cache)
			if err := producer.VisitStep(ownedApplicationStep(&workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./broken", Pos: &workflowlang.Pos{Line: 1, Col: 1}}}})); err != nil || len(producer.Errs()) != 1 {
				t.Fatalf("real parse producer failed: %v", err)
			}
			parse := producer.Errs()[0]
			retained := &findingmodel.Finding{Message: "entry observation"}
			displayPath := filepath.Join("broken", "action.yml")
			composed := composition.Result{Sources: map[string][]byte{displayPath: []byte("different consumer bytes")}, SourceDocuments: map[string]actionlocal.SourceDocument{displayPath: document}}
			if missing {
				composed.Sources[displayPath] = metadata.Bytes()
				delete(composed.SourceDocuments, displayPath)
			}
			registry, err := documentparse.NewSourceSnapshots(fixture.Store)
			if err != nil {
				t.Fatal(err)
			}
			session := analysisSession{ctx: t.Context(), captures: fixture.Store, snapshots: registry, workingDir: root}
			references := map[string]workflowfacts.SourceIndex{}
			rejected, sourceErr := session.admitSourceDocuments(composed, references)
			if sourceErr == nil || !rejected[displayPath] || !rejected[metadata.Revision().Document()] || len(references) != 0 {
				t.Fatalf("bad source retained canonical admission: rejected=%v references=%v error=%v", rejected, references, sourceErr)
			}
			if err := fixture.Store.VerifyHandle(metadata); err != nil {
				t.Fatalf("consumer failure quarantined the shared healthy capture: %v", err)
			}
			if !missing {
				if _, ok := errors.AsType[*capture.ConflictError](sourceErr); !ok {
					t.Fatalf("Verify mismatch lost typed cause: %v", sourceErr)
				}
			}
			accepted, admissionErr := session.admitReportedSources([]*findingmodel.Finding{parse, retained}, "ci.yml", entryPath, composed, rejected)
			if err := fixture.Store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := fixture.Store.Sources()
			if err != nil {
				t.Fatal(err)
			}
			result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: entryPath, Content: entry.Bytes()}, errors: accepted}}}
			err = result.revalidateCaptures(fixture.Store, rows, root, errors.Join(sourceErr, admissionErr))
			result.collectDiagnostics()
			rejection, ok := errors.AsType[*admissionRejectedError](err)
			if !ok || rejection.Count() != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != retained.Message {
				t.Fatalf("failed source leaked its real parse finding: diagnostics=%+v error=%v", result.Diagnostics, err)
			}
		})
	}
}
