package application

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
	"actionlint.kjanat.dev/v2/source"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type unrelatedPathAnalyzer struct{ path string }

func (unrelatedPathAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "unrelated-path-probe", Requires: []analyzer.Fact{analyzer.Source}}
}

func (a unrelatedPathAnalyzer) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return []diagnostic.Diagnostic{{Path: a.path, Message: "foreign observation", Start: diagnostic.Position{Line: 1, Column: 1}}, {Message: "entry observation", Start: diagnostic.Position{Line: 1, Column: 1}}}, nil
}

func TestAnalyzerForeignPathWithoutSourceCannotClaimEntryIdentity(t *testing.T) {
	root := t.TempDir()
	result, err := Analyze(t.Context(), Options{WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: unrelatedPathAnalyzer{path: filepath.Join(root, "unrelated.sh")}}}}, []sourceUnit{{Path: "ci.yml", Content: []byte(commandGoodWorkflow)}})
	rejection, ok := errors.AsType[*admissionRejectedError](err)
	if !ok || rejection.Count() != 1 || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "entry observation" || result.Diagnostics[0].Reference == nil {
		t.Fatalf("foreign path borrowed entry identity or entry finding was lost: result=%+v error=%v", result, err)
	}
}

func TestPublicationRejectsForeignPathWithoutSource(t *testing.T) {
	root := t.TempDir()
	store, fixture := capturefixture.NewCapturedSources(t, root)
	path := filepath.Join(root, "ci.yml")
	entry, err := fixture.Capture(path, []byte(commandGoodWorkflow), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	retained := &findingmodel.Finding{Path: "ci.yml", Message: "entry"}
	entryReference := source.Reference{Document: entry.Revision().Document(), Revision: entry.Revision().Digest()}
	result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: path}, errors: []*findingmodel.Finding{
		{Path: filepath.Join(root, "foreign.sh"), Message: "unmarked foreign"},
		{Path: filepath.Join(root, "another.sh"), Message: "false entry claim", Reference: &entryReference},
		retained,
	}}}}
	err = result.revalidateCaptures(store, rows, root, nil)
	rejection, ok := errors.AsType[*admissionRejectedError](err)
	if !ok || rejection.Count() != 2 || len(result.files[0].errors) != 1 || result.files[0].errors[0] != retained {
		t.Fatalf("publication admitted foreign entry claims: findings=%v error=%v", result.files[0].errors, err)
	}
}

func TestUnavailableReportedSourceIgnoresAnotherCapturedRevision(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed reread", true: "failed reread"}[failed], func(t *testing.T) {
			root := t.TempDir()
			entryPath, reportedPath := filepath.Join(root, "ci.yml"), filepath.Join(root, "library.sh")
			store, fixture := capturefixture.NewCapturedSources(t, root)
			entry, err := fixture.Capture(entryPath, []byte(commandGoodWorkflow), source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := fixture.Capture(reportedPath, []byte("previous bytes\n"), source.RawLF)
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("changed since ShellCheck ran\n")
			if failed {
				content = []byte{}
			}
			finding := &findingmodel.Finding{Path: reportedPath, Rule: "shellcheck", Message: "reported", Start: diagnostic.Position{Line: 1, Column: 1}, Source: content, RawScriptSource: true, UnavailableSource: findingmodel.ReportedSourceUnavailable("shellcheck", reportedPath, failed)}
			session := analysisSession{ctx: t.Context(), captures: store, workingDir: root}
			accepted, err := session.admitReportedSources([]*findingmodel.Finding{finding}, "ci.yml", entryPath, composition.Result{}, nil)
			if err != nil || len(accepted) != 1 || finding.UnavailableSource.Document() != previous.Revision().Document() {
				t.Fatalf("reported alias admission failed: %v %v", accepted, err)
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil {
				t.Fatal(err)
			}
			result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: entryPath, Content: entry.Bytes()}, errors: accepted}}}
			if err := result.revalidateCaptures(store, rows, root, nil); err != nil {
				t.Fatal(err)
			}
			result.collectDiagnostics()
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Reference != nil || len(result.Diagnostics[0].Fixes) != 0 || result.Diagnostics[0].End != finding.Start {
				t.Fatalf("unavailable source promoted another consumer's capture: %+v", result.Diagnostics)
			}
			encoded, err := json.Marshal(result.Diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{`"reference"`, `"revision"`, `"revisions"`, `"fixes"`, previous.Revision().Digest()} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("published unavailable evidence %q: %s", forbidden, encoded)
				}
			}
			if failed && result.Diagnostics[0].Snippet != "" {
				t.Fatal("failed reread borrowed a snippet")
			}
		})
	}
}

func TestReportedSourceAdmissionRejectsUnmarkedForeignSourcesExactly(t *testing.T) {
	root := t.TempDir()
	store, _ := capturefixture.NewCapturedSources(t, root)
	session := analysisSession{ctx: t.Context(), captures: store, workingDir: root}
	retained := &findingmodel.Finding{Message: "entry observation"}
	findings := []*findingmodel.Finding{
		{Path: filepath.Join(root, "foreign.yml"), Source: []byte("foreign yaml")},
		{Path: filepath.Join(root, "foreign.sh"), Source: []byte("foreign script"), RawScriptSource: true},
		retained,
	}
	accepted, err := session.admitReportedSources(findings, "ci.yml", filepath.Join(root, "ci.yml"), composition.Result{}, nil)
	rejection, ok := errors.AsType[*admissionRejectedError](err)
	if !ok || rejection.Count() != 2 || rejection.Code() != "admission-rejected" || len(accepted) != 1 || accepted[0] != retained {
		t.Fatalf("unmarked findings silently escaped or unrelated finding lost: accepted=%v error=%v", accepted, err)
	}
}

func TestQuarantinedDependencyWithholdsReferenceLessAndUnavailableFindings(t *testing.T) {
	root := t.TempDir()
	entryPath, dependencyPath := filepath.Join(root, "ci.yml"), filepath.Join(root, "dependency.yml")
	store, fixture := capturefixture.NewCapturedSources(t, root)
	entry, err := fixture.Capture(entryPath, []byte(commandGoodWorkflow), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := fixture.Capture(dependencyPath, []byte("name: dependency\n"), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := &findingmodel.Finding{Path: dependencyPath, Source: []byte("unverified"), RawScriptSource: true, UnavailableSource: findingmodel.ReportedSourceUnavailable("shellcheck", dependencyPath, false)}
	session := analysisSession{ctx: t.Context(), captures: store, workingDir: root}
	accepted, err := session.admitReportedSources([]*findingmodel.Finding{unavailable}, "ci.yml", entryPath, composition.Result{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflict := store.Quarantine(dependency, "conflicting dependency declaration")
	if conflict == nil {
		t.Fatal("dependency was not quarantined")
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	retained := &findingmodel.Finding{Message: "entry observation"}
	accepted = append(accepted, &findingmodel.Finding{Path: dependencyPath, Source: dependency.Bytes(), Message: "reference-less dependency"}, retained)
	result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: entryPath, Content: entry.Bytes()}, errors: accepted}}}
	err = result.revalidateCaptures(store, rows, root, nil)
	rejection, ok := errors.AsType[*admissionRejectedError](err)
	if !errors.Is(err, conflict) || !ok || rejection.Count() != 2 || len(result.files[0].errors) != 1 || result.files[0].errors[0] != retained {
		t.Fatalf("quarantined dependency findings remained publishable: findings=%v error=%v", result.files[0].errors, err)
	}
}

func TestDependencyVerifyFailureRejectsReferenceLessFindings(t *testing.T) {
	root := t.TempDir()
	dependencyPath := filepath.Join(root, "dependency.yml")
	store, fixture := capturefixture.NewCapturedSources(t, root)
	dependency, err := fixture.Capture(dependencyPath, []byte("captured"), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(dependency, []byte("different consumer bytes")); err == nil {
		t.Fatal("mismatching dependency bytes were accepted")
	}
	rejected := map[string]bool{}
	sourceRejected(rejected, dependencyPath, dependency)
	retained := &findingmodel.Finding{Message: "entry observation"}
	session := analysisSession{ctx: t.Context(), captures: store, workingDir: root}
	accepted, err := session.admitReportedSources([]*findingmodel.Finding{{Path: dependencyPath, Source: dependency.Bytes()}, retained}, "ci.yml", filepath.Join(root, "ci.yml"), composition.Result{}, rejected)
	rejection, ok := errors.AsType[*admissionRejectedError](err)
	if !ok || rejection.Count() != 1 || len(accepted) != 1 || accepted[0] != retained {
		t.Fatalf("failed dependency admitted findings: %v %v", accepted, err)
	}
}

func TestMetadataParseSourceAdmissionUsesCapturedRevision(t *testing.T) {
	for _, state := range []string{"captured", "rejected", "stale", "quarantined", "foreign captured"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			entryPath, metadataPath := filepath.Join(root, "ci.yml"), filepath.Join(root, "broken", "action.yml")
			store, fixture := capturefixture.NewCapturedSources(t, root)
			entry, err := fixture.Capture(entryPath, []byte(commandGoodWorkflow), source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := fixture.Capture(metadataPath, []byte("name: [\n"), source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			cache := actionlocal.NewCache(t.Context(), root, nil)
			cache.Configure(actionlocal.Options{Captures: store, Context: t.Context(), WorkingDir: root})
			lookup, _ := cache.LookupMetadata("./broken")
			document, exists := cache.SourceDocument(lookup)
			if !exists || lookup.Status() != actionlocal.MetadataParseFailed {
				t.Fatal("fixture metadata lost its captured parse failure")
			}
			composed := composition.Result{
				Sources:         map[string][]byte{metadataPath: metadata.Bytes()},
				SourceDocuments: map[string]actionlocal.SourceDocument{metadataPath: document},
			}
			target := metadata.Revision()
			rejected := map[string]bool{}
			switch state {
			case "rejected":
				sourceRejected(rejected, metadataPath, metadata)
			case "stale":
				target, err = source.NewRevision(target.Document(), strings.Repeat("a", 64))
				if err != nil {
					t.Fatal(err)
				}
			case "quarantined":
				if err := store.Quarantine(metadata, "conflicting metadata declarations"); err == nil {
					t.Fatal("metadata was not quarantined")
				}
			case "foreign captured":
				foreign, err := fixture.Capture(filepath.Join(root, "unconsulted.yml"), []byte("name: [\n"), source.YAML)
				if err != nil {
					t.Fatal(err)
				}
				target = foreign.Revision()
			}
			parse := &findingmodel.Finding{Rule: "action", Message: "cannot parse action metadata", MetadataParseSource: target}
			retained := &findingmodel.Finding{Message: "entry observation"}
			session := analysisSession{ctx: t.Context(), captures: store, workingDir: root}
			accepted, admissionErr := session.admitReportedSources([]*findingmodel.Finding{parse, retained}, "ci.yml", entryPath, composed, rejected)
			if parse.MetadataParseSource != target {
				t.Fatal("admission rewrote producer provenance")
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil {
				t.Fatal(err)
			}
			result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: entryPath, Content: entry.Bytes()}, errors: accepted}}}
			err = result.revalidateCaptures(store, rows, root, admissionErr)
			result.collectDiagnostics()
			if state == "captured" {
				if err != nil || len(result.Diagnostics) != 2 || result.Diagnostics[0].Message != parse.Message {
					t.Fatalf("valid parse finding was lost: diagnostics=%+v error=%v", result.Diagnostics, err)
				}
				return
			}
			rejection, ok := errors.AsType[*admissionRejectedError](err)
			if !ok || rejection.Count() != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != retained.Message {
				t.Fatalf("invalid metadata provenance escaped or unrelated finding lost: diagnostics=%+v error=%v", result.Diagnostics, err)
			}
		})
	}
}
