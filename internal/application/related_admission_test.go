package application

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/related"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/source"
)

func TestSealedRelatedAdmissionPreservesPrimaryAndCountsExactly(t *testing.T) {
	root := t.TempDir()
	store, fixture := capturefixture.NewCapturedSources(t, root)
	path := filepath.Join(root, "ci.yml")
	entry, err := fixture.Capture(path, []byte("a\r\n"), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := fixture.Capture("bad.sh", []byte{0xff}, source.RawLF)
	if err != nil {
		t.Fatal(err)
	}
	conflicted, err := fixture.Capture("conflict.yml", []byte("x"), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(t.Context(), filepath.Join(root, "conflict.yml"), source.RawLF); err == nil {
		t.Fatal("missing conflict")
	}
	failedPath := filepath.Join(root, "missing.yml")
	failedDocument, err := store.Canonical(t.Context(), failedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(t.Context(), failedPath, source.YAML); err == nil {
		t.Fatal("missing failed capture")
	}
	second, err := fixture.Capture("second.yml", []byte("a"), source.YAML)
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
	states := map[string]capture.State{}
	for _, row := range rows {
		states[row.Document] = row.State
	}
	if states[failedDocument] != capture.Failed || states[conflicted.Revision().Document()] != capture.Conflicted {
		t.Fatal("fixture did not seal failed/conflicted rows")
	}
	wrongDigest, err := source.NewRevision(entry.Revision().Document(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	failedRevision, err := source.NewRevision(failedDocument, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	good := diagnostic.Related{Message: "kept", Path: "ci.yml", Revision: entry.Revision(), Coordinates: source.YAML, Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2}}
	eof := good
	eof.Start = diagnostic.Position{Line: 2, Column: 1}
	eof.End = eof.Start
	absentRevision, err := source.NewRevision(filepath.ToSlash(filepath.Join(root, "absent.yml")), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	bad := []diagnostic.Related{good, good, good, good, good, good, good, good, good, good, good, good, good}
	bad[0].Revision = wrongDigest
	bad[1].Coordinates = source.RawLF
	bad[2].Path = "foreign.yml"
	bad[3].End = diagnostic.Position{Line: 3, Column: 1}
	bad[4].Start, bad[4].End = good.End, good.Start
	bad[5].Start = diagnostic.Position{Line: 1, Column: 3}
	bad[6].Revision, bad[6].Coordinates, bad[6].Path = invalid.Revision(), source.RawLF, "bad.sh"
	bad[7].Revision, bad[7].Path = conflicted.Revision(), "conflict.yml"
	bad[8].Revision, bad[8].Path = failedRevision, "missing.yml"
	bad[9].Revision = source.Revision{}
	bad[10].Coordinates = ""
	bad[11].Revision, bad[11].Path = absentRevision, "absent.yml"
	bad[12].Revision = second.Revision()
	primary := &findingmodel.Finding{Path: "ci.yml", Message: "primary", Related: append(append([]diagnostic.Related{good, eof}, bad...), diagnostic.Related{Path: "ci.yml"})}
	result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: path}, errors: []*findingmodel.Finding{primary}}}}
	err = result.revalidateCaptures(store, rows, root, nil)
	rejected, ok := errors.AsType[*related.RejectedError](err)
	if !ok || rejected.Count() != len(bad)+1 || len(result.files[0].errors) != 1 || result.files[0].errors[0] != primary || len(primary.Related) != 2 || primary.Related[0] != good || primary.Related[1] != eof {
		t.Fatalf("related admission: primary=%+v error=%v", primary, err)
	}
	if _, ok := store.Revision(wrongDigest); ok {
		t.Fatal("stale revision snapshot admitted")
	}
	if _, ok := store.Revision(conflicted.Revision()); ok {
		t.Fatal("conflicted snapshot admitted")
	}
}

type relatedAdmissionProbe struct{}

func (relatedAdmissionProbe) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "related-admission-probe", Requires: []analyzer.Fact{analyzer.Source}}
}

func (relatedAdmissionProbe) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	point := diagnostic.Position{Line: 1, Column: 1}
	valid := diagnostic.Related{Path: input.Path, Start: point, End: point}
	foreign := valid
	foreign.Path = "other.yml"
	badRange := valid
	badRange.Revision = input.Revision
	badRange.Coordinates = input.Coordinates
	badRange.End.Line = 999
	badModel := valid
	badModel.Revision = input.Revision
	badModel.Coordinates = source.RawLF
	return []diagnostic.Diagnostic{{Path: input.Path, Message: "primary", Start: point, End: point, Related: []diagnostic.Related{valid, foreign, badRange, badModel}}}, nil
}

func TestRelatedBridgeAndSealProduceOneExactTotal(t *testing.T) {
	result, err := Analyze(t.Context(), Options{WorkingDir: t.TempDir(), Analyzers: []analyzer.Registration{{Analyzer: relatedAdmissionProbe{}}}}, []sourceUnit{{Path: "ci.yml", Content: []byte(commandGoodWorkflow)}})
	rejected, ok := errors.AsType[*related.RejectedError](err)
	if !ok || rejected.Count() != 3 || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "primary" || len(result.Diagnostics[0].Related) != 1 || !result.Diagnostics[0].Related[0].Revision.Valid() {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
