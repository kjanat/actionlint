package checking

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results"
)

type suppressionCompletionProbe struct{ failure error }

func (suppressionCompletionProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "completion-probe", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}
}

func (probe suppressionCompletionProbe) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	if input.Path == "failing.yml" {
		return nil, probe.failure
	}
	return nil, nil
}

func TestUnusedSuppressionsRequireCompletedAnalysis(t *testing.T) {
	t.Parallel()
	const prefix = "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n"
	cause := errors.New("scanner unavailable")
	result, err := Analyze(t.Context(), Options{
		WorkingDir: t.TempDir(),
		Analyzers:  []checkinputs.Registration{{Checker: suppressionCompletionProbe{cause}}},
	}, []workspace.Source{
		{Path: "failing.yml", Content: []byte(prefix + "      - run: echo ok # ignore completion-probe\npermissions: {}\n")},
		{Path: "complete.yml", Content: []byte(prefix + "      - run: echo ok # ignore expression-reference\npermissions: {}\n")},
	})
	if !errors.Is(err, cause) || result == nil {
		t.Fatalf("lost partial result or analyzer failure: result=%v error=%v", result, err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "unused-suppression" || result.Diagnostics[0].Path != "complete.yml" || len(result.Diagnostics[0].Fixes) != 1 {
		t.Fatalf("incomplete source produced cleanup, or completed source lost cleanup: %+v", result.Diagnostics)
	}
}

func TestUnusedSuppressionsRequireParsedDeclarations(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		"on: [ # ignore expression-reference\n",
		"on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps: false # ignore expression-reference\n",
	} {
		t.Run(strings.TrimSpace(content), func(t *testing.T) {
			t.Parallel()
			result, err := Analyze(t.Context(), Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "workflow.yml", Content: []byte(content)}})
			if err != nil || result == nil {
				t.Fatalf("lost syntax result: %v", err)
			}
			var syntax bool
			for _, finding := range result.Diagnostics {
				syntax = syntax || finding.Rule == "syntax-check"
				if finding.Rule == "unused-suppression" {
					t.Errorf("parse failure became proof of an unused selector: %+v", finding)
				}
			}
			if !syntax {
				t.Fatal("parse failure was lost", result.Diagnostics)
			}
		})
	}
}

func TestUnavailableOptionalToolRetainsSuppressions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missing := filepath.Join(root, "scanner-not-installed")
	for _, tc := range []struct {
		name, config, replacement string
		unused                    []string
	}{
		{"unavailable", "{}", "# ignore shellcheck", []string{"expression-reference"}},
		{"disabled", "tools: {shellcheck: false}", "", []string{"shellcheck", "expression-reference"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := ConfigurationRules().Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), Options{
				WorkingDir: root,
				Tools: registry.Registrations(registry.Selections{"shellcheck": {
					Command: "shellcheck", Process: &processconfig.Options{Executable: &missing, Optional: true},
				}}),
			}, []workspace.Source{{Path: "workflow.yml", Config: cfg, Content: []byte("on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok # ignore shellcheck expression-reference\npermissions: {}\n")}})
			if err != nil || result == nil {
				t.Fatalf("optional tool became a fatal error: %v", err)
			}
			want := make(map[string]bool)
			for _, selector := range tc.unused {
				want["inline suppression of \""+selector+"\" did not match any findings"] = true
			}
			if len(result.Diagnostics) != len(want) {
				t.Fatalf("unavailable scanner changed cleanup of completed checks: %+v", result.Diagnostics)
			}
			for _, finding := range result.Diagnostics {
				if finding.Rule != "unused-suppression" || !want[finding.Message] || len(finding.Fixes) != 1 {
					t.Fatalf("unexpected cleanup: %+v", finding)
				}
				delete(want, finding.Message)
				fix := finding.Fixes[0]
				if len(fix.Edits) != 1 || fix.Edits[0].Replacement != tc.replacement {
					t.Fatalf("cleanup removed an unavailable selector: %+v", fix)
				}
			}
		})
	}
}

func TestUnavailableToolRetainsNestedActionSuppression(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSuppressionFixture(t, root, ".git/HEAD", "ref: refs/heads/master\n")
	path := writeSuppressionFixture(t, root, "workflow.yml", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n      - uses: ./inner\npermissions: {}\n")
	writeSuppressionFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok # ignore shellcheck expression-reference\n")
	missing := filepath.Join(root, "scanner-not-installed")
	result, err := Check(t.Context(), Options{
		WorkingDir: root,
		Tools: registry.Registrations(registry.Selections{"shellcheck": {
			Command: "shellcheck", Process: &processconfig.Options{Executable: &missing, Optional: true},
		}}),
	}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
	if err != nil || result == nil {
		t.Fatalf("optional tool became a fatal error: %v", err)
	}
	seen := make(map[invocation.ID]bool)
	var entryDocument, entryRevision, actionDocument, actionRevision string
	for _, check := range result.IncompleteChecks {
		if check.Rule != "shellcheck" {
			continue
		}
		if check.Invocation.ID == "" || check.Invocation.Entry.Document == "" || check.Invocation.Entry.Revision == "" {
			t.Fatalf("known invocation lost its entry identity: %+v", check)
		}
		if entryDocument == "" {
			entryDocument, entryRevision = check.Invocation.Entry.Document, check.Invocation.Entry.Revision
		}
		if check.Invocation.Entry.Document != entryDocument || check.Invocation.Entry.Revision != entryRevision {
			t.Fatalf("calls were assigned to different entry revisions: %+v", result.IncompleteChecks)
		}
		if check.Source.Document == entryDocument {
			continue
		}
		if actionDocument == "" {
			actionDocument, actionRevision = check.Source.Document, check.Source.Revision
		}
		if check.Source.Document != actionDocument || check.Source.Revision != actionRevision || seen[check.Invocation.ID] {
			t.Fatalf("repeated calls lost their shared source or distinct IDs: %+v", result.IncompleteChecks)
		}
		seen[check.Invocation.ID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("want two incomplete action invocations, got %+v", result.IncompleteChecks)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("repeated action lost scoped cleanup: %+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Rule != "unused-suppression" || finding.Path != filepath.Join("inner", "action.yml") || finding.Message != `inline suppression of "expression-reference" did not match any findings` || len(finding.Fixes) != 1 {
		t.Fatalf("unavailable scanner changed nested action cleanup: %+v", finding)
	}
	fix := finding.Fixes[0]
	if len(fix.Edits) != 1 || fix.Edits[0].Path != finding.Path || fix.Edits[0].Replacement != "# ignore shellcheck" {
		t.Fatalf("cleanup removed the nested action's unavailable selector: %+v", fix)
	}
}
