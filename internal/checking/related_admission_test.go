package checking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext/capture"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestSealedRelatedAdmissionPreservesPrimaryAndCountsExactly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, fixture := capturefixture.NewCapturedSources(t, root)
	path := filepath.Join(root, "ci.yml")
	entry, err := fixture.Capture(path, []byte("a\r\n"), sourcetext.YAML)
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := fixture.Capture("bad.sh", []byte{0xff}, sourcetext.RawLF)
	if err != nil {
		t.Fatal(err)
	}
	conflicted, err := fixture.Capture("conflict.yml", []byte("x"), sourcetext.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(t.Context(), filepath.Join(root, "conflict.yml"), sourcetext.RawLF); err == nil {
		t.Fatal("missing conflict")
	}
	failedPath := filepath.Join(root, "missing.yml")
	failedDocument, err := store.Canonical(t.Context(), failedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(t.Context(), failedPath, sourcetext.YAML); err == nil {
		t.Fatal("missing failed capture")
	}
	second, err := fixture.Capture("second.yml", []byte("a"), sourcetext.YAML)
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
	wrongDigest, err := sourcetext.NewRevision(entry.Revision().Document(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	failedRevision, err := sourcetext.NewRevision(failedDocument, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	good := results.Related{Message: "kept", Path: "ci.yml", Revision: entry.Revision(), Coordinates: sourcetext.YAML, Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 2}}
	eof := good
	eof.Start = sourcetext.Position{Line: 2, Column: 1}
	eof.End = eof.Start
	absentRevision, err := sourcetext.NewRevision(filepath.ToSlash(filepath.Join(root, "absent.yml")), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	bad := []results.Related{good, good, good, good, good, good, good, good, good, good, good, good, good}
	bad[0].Revision = wrongDigest
	bad[1].Coordinates = sourcetext.RawLF
	bad[2].Path = "foreign.yml"
	bad[3].End = sourcetext.Position{Line: 3, Column: 1}
	bad[4].Start, bad[4].End = good.End, good.Start
	bad[5].Start = sourcetext.Position{Line: 1, Column: 3}
	bad[6].Revision, bad[6].Coordinates, bad[6].Path = invalid.Revision(), sourcetext.RawLF, "bad.sh"
	bad[7].Revision, bad[7].Path = conflicted.Revision(), "conflict.yml"
	bad[8].Revision, bad[8].Path = failedRevision, "missing.yml"
	bad[9].Revision = sourcetext.Revision{}
	bad[10].Coordinates = ""
	bad[11].Revision, bad[11].Path = absentRevision, "absent.yml"
	bad[12].Revision = second.Revision()
	primary := &finding.Finding{Path: "ci.yml", Message: "primary", Related: append(append([]results.Related{good, eof}, bad...), results.Related{Path: "ci.yml"})}
	result := analysisResult{files: []analyzedFile{{captured: entry, source: sourceUnit{Path: "ci.yml", InputPath: path}, errors: []*finding.Finding{primary}}}}
	err = result.revalidateCaptures(store, rows, root, nil)
	rejected, ok := errors.AsType[*results.RejectedRelatedError](err)
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

func (relatedAdmissionProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "related-admission-probe", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Source}}
}

func (relatedAdmissionProbe) CheckDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	point := sourcetext.Position{Line: 1, Column: 1}
	valid := results.Related{Path: input.Path, Start: point, End: point}
	foreign := valid
	foreign.Path = "other.yml"
	badRange := valid
	badRange.Revision = input.Revision
	badRange.Coordinates = input.Coordinates
	badRange.End.Line = 999
	badModel := valid
	badModel.Revision = input.Revision
	badModel.Coordinates = sourcetext.RawLF
	return []results.Diagnostic{{Path: input.Path, Message: "primary", Start: point, End: point, Related: []results.Related{valid, foreign, badRange, badModel}}}, nil
}

func TestRelatedBridgeAndSealProduceOneExactTotal(t *testing.T) {
	t.Parallel()
	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		testRelatedBridgeAndSeal(t, t.TempDir())
	})
	t.Run("symlinked directory", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join(t.TempDir(), "root-alias")
		if err := os.Symlink(t.TempDir(), root); err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skipf("symlink privilege unavailable: %v", err)
			}
			t.Fatal(err)
		}
		testRelatedBridgeAndSeal(t, root)
	})
}

func testRelatedBridgeAndSeal(t *testing.T, root string) {
	t.Helper()
	result, err := Analyze(t.Context(), Options{WorkingDir: root, Analyzers: []checkinputs.Registration{{Checker: relatedAdmissionProbe{}}}}, []sourceUnit{{Path: "ci.yml", Content: []byte(commandGoodWorkflow)}})
	rejected, ok := errors.AsType[*results.RejectedRelatedError](err)
	if !ok || rejected.Count() != 3 || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "primary" || len(result.Diagnostics[0].Related) != 1 || !result.Diagnostics[0].Related[0].Revision.Valid() {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
