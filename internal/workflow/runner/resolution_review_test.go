package runner

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestProviderPointerResolutionsMatchValues(t *testing.T) {
	resolved := runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "queue", OS: runnerapi.Windows, DefaultShell: "pwsh", CaseSensitivity: runnerapi.CaseInsensitive}}}
	noMatch := runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "absent"}}}
	unknown := runnerapi.Unknown{Reason: "runtime"}
	notApplicable := runnerapi.NotApplicable{}
	for _, tc := range []struct {
		name           string
		value, pointer runnerapi.Resolution
	}{
		{"resolved", resolved, &resolved},
		{"no-match", noMatch, &noMatch},
		{"unknown", unknown, &unknown},
		{"not-applicable", notApplicable, &notApplicable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := runnerWorkflow(t, "queue")
			prepare := func(resolution runnerapi.Resolution) *Knowledge {
				p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
					return []runnerapi.Answer{{Resolution: resolution, Evidence: runnerapi.Evidence{Dataset: "v1", Scope: r.Scope, Freshness: runnerapi.Fresh, Complete: true}}}, nil
				}}
				k, err := Prepare(t.Context(), workflow.Snapshot, nil, runnerapi.Options{Target: "buildkite", Providers: []runnerapi.Registration{{Provider: p}}}, "scope")
				if err != nil {
					t.Fatal(err)
				}
				return k
			}
			value, pointer := prepare(tc.value), prepare(tc.pointer)
			if !reflect.DeepEqual(value.Report, pointer.Report) || !reflect.DeepEqual(value.Job(workflow.Views["test"]), pointer.Job(workflow.Views["test"])) {
				t.Fatalf("pointer changed semantic evidence: value=%+v pointer=%+v", value, pointer)
			}
		})
	}
}

func TestProviderPointerResolutionValidation(t *testing.T) {
	for _, resolution := range []runnerapi.Resolution{(*runnerapi.Resolved)(nil), (*runnerapi.NoMatch)(nil), (*runnerapi.Unknown)(nil), (*runnerapi.NotApplicable)(nil), &runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "wrong source", Position: diagnostic.Position{Line: 99, Column: 1}}}}} {
		p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
			return []runnerapi.Answer{{Resolution: resolution, Evidence: runnerapi.Evidence{Dataset: "v1", Scope: r.Scope, Freshness: runnerapi.Fresh, Complete: true}}}, nil
		}}
		if _, err := Prepare(t.Context(), runnerWorkflow(t, "queue").Snapshot, nil, runnerapi.Options{Target: "buildkite", Providers: []runnerapi.Registration{{Provider: p}}}, "scope"); err == nil {
			t.Fatalf("accepted invalid pointer resolution: %#v", resolution)
		}
	}
}

func TestProviderPointerResolvedCannotCompletePartialMatrix(t *testing.T) {
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix:\n        os: [windows-latest]\n        unknown: '${{ fromJSON(vars.AXIS) }}'\n    runs-on: ${{ matrix.os }}\n    steps: [{run: echo ok}]\n")
	p := &testProvider{resolve: func(r runnerapi.Request) ([]runnerapi.Answer, error) {
		answers := make([]runnerapi.Answer, len(r.Selectors))
		for i := range answers {
			answers[i] = runnerapi.Answer{Resolution: &runnerapi.Resolved{Candidates: []runnerapi.Candidate{{OS: runnerapi.Windows, DefaultShell: "pwsh"}}}, Evidence: runnerapi.Evidence{Dataset: "v1", Scope: r.Scope, Freshness: runnerapi.Fresh, Complete: true}}
		}
		return answers, nil
	}}
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{Target: "buildkite", Providers: []runnerapi.Registration{{Provider: p}}}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	partial := 0
	for i, selector := range p.request.Selectors {
		if !selector.Incomplete {
			continue
		}
		partial++
		unknown, ok := k.Report.Answers[i].Resolution.(runnerapi.Unknown)
		if !ok || !strings.Contains(unknown.Reason, "incomplete") {
			t.Fatalf("pointer supplied false completeness: %+v", k.Report.Answers[i])
		}
	}
	if partial == 0 || k.Job(w.Views["test"]).Shell != "" {
		t.Fatalf("partial matrix inferred a shell: %+v", k)
	}
}

func TestBuiltinMacOSCaseRequiresHostedImage(t *testing.T) {
	for _, tc := range []struct {
		selector string
		os       runnerapi.OS
		caseMode runnerapi.CaseSensitivity
	}{
		{"macos", runnerapi.MacOS, runnerapi.UnknownCase},
		{"[self-hosted, macos]", runnerapi.MacOS, runnerapi.UnknownCase},
		{"[self-hosted, macos, x64]", runnerapi.MacOS, runnerapi.UnknownCase},
		{"macos-latest", runnerapi.MacOS, runnerapi.CaseInsensitive},
		{"xcode-27", runnerapi.MacOS, runnerapi.CaseInsensitive},
		{"[self-hosted, linux]", runnerapi.Linux, runnerapi.CaseSensitive},
		{"[self-hosted, windows]", runnerapi.Windows, runnerapi.CaseInsensitive},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			w := runnerWorkflow(t, tc.selector)
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			resolved, ok := k.Report.Answers[0].Resolution.(runnerapi.Resolved)
			if !ok || len(resolved.Candidates) != 1 {
				t.Fatalf("resolution=%+v", k.Report.Answers[0])
			}
			candidate := resolved.Candidates[0]
			known := tc.caseMode != runnerapi.UnknownCase
			job := w.Views["test"]
			if candidate.OS != tc.os || candidate.CaseSensitivity != tc.caseMode || k.Job(job).PathsKnown != known || builtinJob(job).PathsKnown != known {
				t.Fatalf("case guarantee incorrect: candidate=%+v knowledge=%+v direct=%+v", candidate, k.Job(job), builtinJob(job))
			}
			if tc.os == runnerapi.MacOS && (candidate.DefaultShell != "bash" || k.Job(job).Platform != MacOrLinux) {
				t.Fatalf("unknown filesystem case lost OS/shell: %+v", candidate)
			}
		})
	}
}
