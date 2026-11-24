package checking_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/backends"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

type availabilityBackend struct{ unavailable bool }

func (b availabilityBackend) Check(context.Context, scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	if b.unavailable {
		return nil, scriptengine.ErrUnavailable
	}
	return nil, nil
}

func TestUnavailableEngineKeepsItsSuppressions(t *testing.T) {
	t.Parallel()
	const workflow = "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello # ignore[lazy-engine]\n        shell: bash\n"
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "available", true: "unavailable"}[unavailable], func(t *testing.T) {
			t.Parallel()
			registration, err := backends.RegisterEngine(scriptengine.Descriptor{Rule: rule.Descriptor{Name: "lazy-engine", Category: "correctness"}, Shells: []string{"bash"}}, availabilityBackend{unavailable: unavailable})
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, Tools: []contract.Registration{registration}}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(workflow)})
			if err != nil {
				t.Fatal(err)
			}
			unused, incomplete := 0, 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "unused-suppression" {
					t.Fatalf("unexpected finding: %+v", diagnostic)
				}
				unused++
			}
			for _, check := range result.IncompleteChecks {
				if check.Rule == "lazy-engine" {
					incomplete++
				}
			}
			if unavailable && (unused != 0 || incomplete == 0) || !unavailable && (unused != 1 || incomplete != 0) {
				t.Fatalf("unused=%d incomplete=%+v", unused, result.IncompleteChecks)
			}
		})
	}
}
