package checking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/results"
)

type pathRetargetChecker struct{ retarget func() error }

func (pathRetargetChecker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "captured-path-test", Category: "correctness", Maturity: "stable", RequiredInputs: []checkinputs.InputKind{checkinputs.Source}}
}

func (checker pathRetargetChecker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checker.retarget(); err != nil {
		return nil, err
	}
	return []results.Diagnostic{{Rule: "captured-path-test", Path: input.Path, Reference: &input.Reference, Start: results.Position{Line: 1}, Message: "captured source"}}, nil
}

func TestResultPathUsesCapturedIdentityAfterSymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "ci.yml"), filepath.Join(t.TempDir(), "other.yml")
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(t.TempDir(), "entry.yml")
	if err := os.Symlink(first, alias); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	checker := pathRetargetChecker{retarget: func() error {
		if err := os.Remove(alias); err != nil {
			return err
		}
		return os.Symlink(second, alias)
	}}
	result, err := Check(t.Context(), Options{WorkingDir: root, SkipProjectConfig: true, Analyzers: []checkinputs.Registration{{Checker: checker}}}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{alias}})
	if err != nil || result == nil || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if got := result.Diagnostics[0]; got.Path != "ci.yml" || got.Reference == nil || filepath.FromSlash(got.Reference.Document) != first {
		t.Fatalf("result followed the retargeted filesystem path: %+v", got)
	}
}
