package actionlint

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

const cachePolicySteps = "    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"

func lintCachePolicy(t *testing.T, source, config string) []*Error {
	t.Helper()
	l, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
	if err != nil {
		t.Fatal(err)
	}
	if config != "" {
		l.defaultConfig, err = ParseConfig([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	errs, err := l.Lint("test.yaml", []byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	return errs
}

func TestCachePolicyTriggers(t *testing.T) {
	for _, group := range []struct {
		events string
		warn   bool
	}{
		{"branch_protection_rule check_run check_suite deployment deployment_status discussion discussion_comment fork gollum image_version issue_comment issues label milestone public pull_request_target status watch workflow_run", true},
		{"push workflow_dispatch repository_dispatch delete registry_package page_build schedule pull_request pull_request_review pull_request_review_comment merge_group create release workflow_call", false},
	} {
		for event := range strings.FieldsSeq(group.events) {
			t.Run(event, func(t *testing.T) {
				// Exercise each event independently of event-specific required fields.
				w := &Workflow{On: []Event{&WebhookEvent{Hook: &String{Value: event}}}, CacheMode: &CacheMode{Kind: CacheModeWrite, Pos: &Pos{Line: 2, Col: 13}}}
				r := NewRuleCacheWriteUntrusted()
				if err := r.VisitWorkflowPre(w); err != nil {
					t.Fatal(err)
				}
				if err := r.VisitJobPre(&Job{}); err != nil {
					t.Fatal(err)
				}
				if (len(r.Errs()) == 1) != group.warn {
					t.Fatalf("want warning=%v, got %v", group.warn, r.Errs())
				}
			})
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
			src := "on: [pull_request_target, push]\n" + tc.workflow + "jobs:\n  test:\n" + tc.job + cachePolicySteps
			errs := lintCachePolicy(t, src, "")
			if tc.line == 0 {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || errs[0].Kind != "cache-write-untrusted" || errs[0].Line != tc.line || !strings.Contains(errs[0].Message, "poison caches") || !strings.Contains(errs[0].Message, `"pull_request_target"`) {
				t.Fatalf("want write policy finding on line %d, got %v", tc.line, errs)
			}
		})
	}
	t.Run("inherited declaration reported once", func(t *testing.T) {
		src := "on: pull_request_target\ncache-mode: write\njobs:\n  a:\n" + cachePolicySteps + "  b:\n" + cachePolicySteps
		if errs := lintCachePolicy(t, src, ""); len(errs) != 1 || errs[0].Line != 2 {
			t.Fatal(errs)
		}
	})
	t.Run("invalid mode has no policy cascade", func(t *testing.T) {
		if errs := lintCachePolicy(t, "on: pull_request_target\ncache-mode: invalid\njobs:\n  a:\n"+cachePolicySteps, ""); len(errs) != 1 || errs[0].Kind != "syntax-check" {
			t.Fatal(errs)
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
					src := "on: pull_request_target\n" + workflow + "jobs:\n  call:\n" + job + "    uses: " + target + "\n"
					errs := lintCachePolicy(t, src, "")
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
					} else if len(errs) != 1 || errs[0].Kind != want {
						t.Fatalf("want %s, got %v", want, errs)
					}
				})
			}
		}
	}
	for _, event := range []string{"push", "pull_request", "workflow_call"} {
		src := "on: " + event + "\njobs:\n  call:\n    uses: example/repo/.github/workflows/build.yaml@main\n"
		if errs := lintCachePolicy(t, src, ""); len(errs) != 0 {
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
		{"read", []string{"save"}},
		{"write", nil},
		{"write-only", []string{"restore"}},
		{"none", []string{"save", "restore", "both"}},
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
					src := "on: push\n" + mode + "jobs:\n  test:\n" + job + "    runs-on: ubuntu-latest\n    steps:\n      - uses: " + action + "@v5\n        with: {path: .cache, key: test}\n"
					errs := lintCachePolicy(t, src, "")
					if slices.Contains(tc.bad, op) {
						if len(errs) != 1 || errs[0].Kind != "cache-operation" || !strings.Contains(errs[0].Message, "GitHub skips the operation") {
							t.Fatal(errs)
						}
						if !strings.Contains(strings.Split(src, "\n")[errs[0].Line-1], "uses:") {
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

func TestCachePolicyConfig(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"cache-write-untrusted", "on: pull_request_target\ncache-mode: write\njobs:\n  test:\n" + cachePolicySteps},
		{"cache-call-unrestricted", "on: pull_request_target\njobs:\n  call:\n    uses: example/repo/.github/workflows/build.yaml@main\n"},
		{"cache-operation", "on: push\ncache-mode: none\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/cache@v5\n        with: {path: .cache, key: test}\n"},
	} {
		for _, value := range []string{"true", "false", "null"} {
			t.Run(tc.name+"/"+value, func(t *testing.T) {
				errs := lintCachePolicy(t, tc.source, fmt.Sprintf("policy: {%s: %s}", tc.name, value))
				if value == "false" {
					if len(errs) != 0 {
						t.Fatal(errs)
					}
				} else if len(errs) != 1 || errs[0].Kind != tc.name {
					t.Fatal(errs)
				}
			})
		}
		for _, value := range []string{"1", "'true'", "{}", "[]"} {
			if _, err := ParseConfig(fmt.Appendf(nil, "policy: {%s: %s}", tc.name, value)); err == nil {
				t.Errorf("accepted invalid %s value %s", tc.name, value)
			}
		}
	}
}
