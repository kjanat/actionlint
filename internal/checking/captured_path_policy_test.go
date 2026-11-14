package checking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

type pathRetargetChecker struct{ retarget func() error }

func (pathRetargetChecker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "captured-path-test", Category: "correctness", Maturity: "stable", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}
}

func (checker pathRetargetChecker) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checker.retarget(); err != nil {
		return nil, err
	}
	return []results.Diagnostic{{Rule: "captured-path-test", Path: input.Path, Reference: &input.Reference, Start: sourcepos.Position{Line: 1}, Message: "captured source"}}, nil
}

func TestResultPathUsesCapturedIdentityAfterSymlinkRetarget(t *testing.T) {
	t.Parallel()
	for _, aliasedRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("aliased-root=%t", aliasedRoot), func(t *testing.T) {
			t.Parallel()
			testResultPathUsesCapturedIdentityAfterSymlinkRetarget(t, aliasedRoot)
		})
	}
}

func testResultPathUsesCapturedIdentityAfterSymlinkRetarget(t *testing.T, aliasedRoot bool) {
	t.Helper()
	root := t.TempDir()
	if aliasedRoot {
		alias := filepath.Join(t.TempDir(), "root-alias")
		if err := os.Symlink(root, alias); err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skipf("symlink privilege unavailable: %v", err)
			}
			t.Fatal(err)
		}
		root = alias
	}
	first, second := filepath.Join(root, "ci.yml"), filepath.Join(t.TempDir(), "other.yml")
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\npermissions: {}\n")
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
	physicalFirst, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	checker := pathRetargetChecker{retarget: func() error {
		if err := os.Remove(alias); err != nil {
			return err
		}
		return os.Symlink(second, alias)
	}}
	result, err := Check(t.Context(), Options{WorkingDir: root, SkipProjectConfig: true, Analyzers: []checkinputs.Registration{{Checker: checker}}}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{alias}})
	if err != nil || result == nil {
		t.Fatalf("result available=%t error=%v", result != nil, err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics=%v", result.Diagnostics)
	}
	display, err := filepath.Rel(root, alias)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Diagnostics[0]; got.Path != display || got.Reference == nil || filepath.FromSlash(got.Reference.Document) != physicalFirst {
		t.Fatalf("result followed the retargeted filesystem path: %+v", got)
	}
}
