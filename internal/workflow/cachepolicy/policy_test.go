package cachepolicy

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/webhook"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

const cachePolicySteps = `    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`

func checkPolicy(t *testing.T, source string) []diagnostic.Diagnostic {
	t.Helper()
	w, findings := workflowlang.ParseDocument([]byte(source))
	if w == nil {
		return findings
	}
	trust := NewTrust(w)
	jobs := make([]*workflowlang.Job, 0, len(w.Jobs))
	for _, j := range w.Jobs {
		jobs = append(jobs, j)
	}
	slices.SortFunc(jobs, func(a, b *workflowlang.Job) int { return a.Pos.Line - b.Pos.Line })
	for _, j := range jobs {
		findings = append(findings, trust.CheckWrites(j)...)
		findings = append(findings, trust.CheckCall(j)...)
		mode := workflowlang.EffectiveCacheMode(w.CacheMode, j.CacheMode)
		for _, step := range j.Steps {
			findings = append(findings, CheckStep(step, mode)...)
		}
	}
	slices.SortStableFunc(findings, func(a, b diagnostic.Diagnostic) int { return a.Start.Line - b.Start.Line })
	return findings
}

func TestCachePolicyTriggers(t *testing.T) {
	for _, group := range []struct {
		events string
		warn   bool
	}{
		{"branch_protection_rule check_run check_suite deployment deployment_status discussion discussion_comment fork gollum image_version issue_comment issues label milestone public pull_request_target status watch workflow_run", true},
		{"push workflow_dispatch repository_dispatch delete registry_package page_build schedule pull_request pull_request_review pull_request_review_comment merge_group create release workflow_call", false},
		{"banana_launch", true},
	} {
		for event := range strings.FieldsSeq(group.events) {
			t.Run(event, func(t *testing.T) {
				// Exercise each event independently of event-specific required fields.
				w := &workflowlang.Workflow{On: []workflowlang.Event{&workflowlang.WebhookEvent{Hook: &workflowlang.String{Value: event}}}, CacheMode: &workflowlang.CacheMode{Kind: workflowlang.CacheModeWrite, Pos: &workflowlang.Pos{Line: 2, Col: 13}}}
				findings := NewTrust(w).CheckWrites(&workflowlang.Job{})
				if (len(findings) == 1) != group.warn {
					t.Fatalf("want warning=%v, got %v", group.warn, findings)
				}
			})
		}
	}
}

func TestCachePolicyEventClassification(t *testing.T) {
	for event := range webhook.AllWebhookTypes {
		class, exists := cacheEventClasses[event]
		if !exists || class == cacheEventUnknown {
			t.Errorf("generated event %q needs an explicit cache trust classification", event)
		}
	}
	for event, class := range cacheEventClasses {
		if _, exists := webhook.AllWebhookTypes[event]; !exists {
			t.Errorf("cache event %q is absent from the generated inventory", event)
		}
		switch class {
		case cacheEventRestricted, cacheEventTrusted, cacheEventRefScoped, cacheEventInherited:
		default:
			t.Errorf("event %q has invalid cache trust classification %d", event, class)
		}
	}
}

func TestCachePolicyEffectiveWrite(t *testing.T) {
	for _, tc := range []struct {
		name, workflow, job string
		line                int
	}{
		{"omission keeps safe default", "", "", 0},
		{"read", "cache-mode: read\n", "", 0},
		{"none", "cache-mode: none\n", "", 0},
		{"workflow writes", "cache-mode: write\n", "", 2},
		{"workflow saves only", "cache-mode: write-only\n", "", 2},
		{"job overrides safe default", "cache-mode: read\n", "    cache-mode: write-only\n", 5},
		{"unused workflow writes", "cache-mode: write\n", "    cache-mode: read\n", 0},
		{"job disables inherited writes", "cache-mode: write\n", "    cache-mode: none\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "on: [pull_request_target, push]\n" + tc.workflow + `jobs:
  test:
` + tc.job + cachePolicySteps
			errs := checkPolicy(t, src)
			if tc.line == 0 {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || errs[0].Rule != "cache-write-untrusted" || errs[0].Start.Line != tc.line || !strings.Contains(errs[0].Message, "poison caches") || !strings.Contains(errs[0].Message, `"pull_request_target"`) {
				t.Fatalf("want write policy finding on line %d, got %v", tc.line, errs)
			}
		})
	}
	t.Run("inherited declaration reported once", func(t *testing.T) {
		src := `on: pull_request_target
cache-mode: write
jobs:
  a:
` + cachePolicySteps + "  b:\n" + cachePolicySteps
		if errs := checkPolicy(t, src); len(errs) != 1 || errs[0].Start.Line != 2 {
			t.Fatal(errs)
		}
	})
	t.Run("invalid mode has no policy cascade", func(t *testing.T) {
		if errs := checkPolicy(t, `on: pull_request_target
cache-mode: invalid
jobs:
  a:
`+cachePolicySteps); len(errs) != 1 || errs[0].Rule != "syntax-check" {
			t.Fatal(errs)
		}
	})
}

func TestCachePolicyWorkflowGrantUsage(t *testing.T) {
	for _, tc := range []struct {
		name, jobs string
		lines      []int
	}{
		{"all read", `  a:
    cache-mode: read
` + cachePolicySteps + `  b:
    cache-mode: read
` + cachePolicySteps, nil},
		{"all none", `  a:
    cache-mode: none
` + cachePolicySteps + `  b:
    cache-mode: none
` + cachePolicySteps, nil},
		{"mixed safe overrides", `  a:
    cache-mode: read
` + cachePolicySteps + `  b:
    cache-mode: none
` + cachePolicySteps, nil},
		{"one inheritor", `  a:
    cache-mode: read
` + cachePolicySteps + "  b:\n" + cachePolicySteps, []int{2}},
		{"unsafe job override", `  a:
    cache-mode: read
` + cachePolicySteps + `  b:
    cache-mode: write-only
` + cachePolicySteps, []int{10}},
		{"inherited and job grants", "  a:\n" + cachePolicySteps + `  b:
    cache-mode: write-only
` + cachePolicySteps, []int{2, 9}},
		{"only reusable inheritors", `  a:
    uses: example/repo/.github/workflows/a.yaml@main
  b:
    uses: example/repo/.github/workflows/b.yaml@main
`, []int{2}},
		{"only reusable safe overrides", `  a:
    cache-mode: read
    uses: example/repo/.github/workflows/a.yaml@main
  b:
    cache-mode: none
    uses: example/repo/.github/workflows/b.yaml@main
`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: pull_request_target
cache-mode: write
jobs:
` + tc.jobs
			var lines []int
			for _, err := range checkPolicy(t, source) {
				column := 17
				if err.Start.Line == 2 {
					column = 13
				}
				if err.Rule != "cache-write-untrusted" || err.Start.Column != column {
					t.Fatalf("unexpected diagnostic: %v", err)
				}
				lines = append(lines, err.Start.Line)
			}
			if !slices.Equal(lines, tc.lines) {
				t.Fatalf("want grant diagnostics on lines %v, got %v", tc.lines, lines)
			}
		})
	}
	t.Run("zero jobs has no effective grant", func(t *testing.T) {
		errs := checkPolicy(t, `on: pull_request_target
cache-mode: write
jobs: {}
`)
		if len(errs) != 1 || errs[0].Rule != "syntax-check" {
			t.Fatalf("expected only the empty jobs syntax diagnostic, got %v", errs)
		}
	})
}

func TestCachePolicyReusableCalls(t *testing.T) {
	for _, target := range []string{"./.github/workflows/build.yaml", "example/repo/.github/workflows/build.yaml@main"} {
		for _, mode := range []string{"", "read", "none", "write", "write-only", "invalid"} {
			for _, level := range []string{"workflow", "job"} {
				t.Run(target+"/"+mode+"/"+level, func(t *testing.T) {
					workflow, job := "", ""
					if mode != "" {
						if level == "workflow" {
							workflow = "cache-mode: " + mode + "\n"
						} else {
							job = "    cache-mode: " + mode + "\n"
						}
					}
					src := "on: pull_request_target\n" + workflow + `jobs:
  call:
` + job + "    uses: " + target + "\n"
					errs := checkPolicy(t, src)
					want := ""
					switch mode {
					case "":
						want = "cache-call-unrestricted"
					case "write", "write-only":
						want = "cache-write-untrusted"
					case "invalid":
						want = "syntax-check"
					}
					if want == "" {
						if len(errs) != 0 {
							t.Fatal(errs)
						}
					} else if len(errs) != 1 || errs[0].Rule != want {
						t.Fatalf("want %s, got %v", want, errs)
					}
				})
			}
		}
	}
	for _, event := range []string{"push", "pull_request", "workflow_call"} {
		src := "on: " + event + `
jobs:
  call:
    uses: example/repo/.github/workflows/build.yaml@main
`
		if errs := checkPolicy(t, src); len(errs) != 0 {
			t.Fatalf("%s: %v", event, errs)
		}
	}
}

func TestCachePolicyOperations(t *testing.T) {
	for _, tc := range []struct {
		mode string
		bad  []string
	}{
		{"", nil},
		{"read", []string{"save"}},                    // Combined cache can still restore.
		{"write", nil},                                // Both operations are granted.
		{"write-only", []string{"restore"}},           // Combined cache can still save.
		{"none", []string{"save", "restore", "both"}}, // Neither operation is granted.
	} {
		for _, op := range []string{"save", "restore", "both"} {
			for _, override := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/override=%v", tc.mode, op, override), func(t *testing.T) {
					mode, job := "", ""
					if tc.mode != "" {
						if override {
							mode = "cache-mode: none\n"
							job = "    cache-mode: " + tc.mode + "\n"
						} else {
							mode = "cache-mode: " + tc.mode + "\n"
						}
					}
					action := "actions/cache"
					if op != "both" {
						action += "/" + op
					}
					src := "on: push\n" + mode + `jobs:
  test:
` + job + `    runs-on: ubuntu-latest
    steps:
      - uses: ` + action + `@v5
        with: {path: .cache, key: test}
`
					errs := checkPolicy(t, src)
					if slices.Contains(tc.bad, op) {
						if len(errs) != 1 || errs[0].Rule != "cache-operation" || !strings.Contains(errs[0].Message, "GitHub skips the operation") {
							t.Fatal(errs)
						}
						if !strings.Contains(strings.Split(src, "\n")[errs[0].Start.Line-1], "uses:") {
							t.Fatalf("finding is not on the cache step: %v", errs)
						}
					} else if len(errs) != 0 {
						t.Fatal(errs)
					}
				})
			}
		}
	}
}

func TestCachePolicyOperationReferences(t *testing.T) {
	for _, tc := range []struct {
		uses string
		warn bool
	}{
		{"actions/cache@v5", true},
		{"ACTIONS/CACHE@v5", true},
		{"AcTiOnS/CaChE/save@v5", true},
		{"AcTiOnS/CaChE/restore@v5", true},
		{"actions/cache/SAVE@v5", false},
		{"actions/cache/Restore@v5", false},
		{"someone/actions-cache@v5", false},
		{"actions/cache-fork@v5", false},
		{"actions/cache/save-extra@v5", false},
		{"actions/cache/restore/extra@v5", false},
		{"actions/cache/@v5", false},
		{"./actions/cache/save", false},
		{"$/actions/cache/restore", false},
		{"actions/cache@", false},
		{"actions/cache", false},
		{"actions/cache@${{ 'v5' }}", false},
	} {
		t.Run(tc.uses, func(t *testing.T) {
			findings := CheckStep(&workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: tc.uses, Pos: &workflowlang.Pos{Line: 7, Col: 15}}}}, &workflowlang.CacheMode{Kind: workflowlang.CacheModeNone})
			if tc.warn {
				if errs := findings; len(errs) != 1 || errs[0].Rule != "cache-operation" || errs[0].Start.Line != 7 || errs[0].Start.Column != 15 {
					t.Fatalf("expected one cache-operation finding on the action, got %v", errs)
				}
			} else if len(findings) != 0 {
				t.Fatalf("reference is outside the official cache entrypoints: %v", findings)
			}
		})
	}
}
