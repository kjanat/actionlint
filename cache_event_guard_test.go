package actionlint

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCachePolicyEventGuards(t *testing.T) {
	for _, tc := range []struct {
		condition string
		events    []string
	}{
		{"github.event_name == 'pull_request'", nil},
		{"github.event_name == 'pull_request' && github.actor == 'trusted'", nil},
		{"github.actor == 'trusted' && github.event_name == 'pull_request'", nil},
		{"github.event_name == 'pull_request' || github.actor == 'trusted'", []string{"issue_comment", "pull_request_target"}},
		{"github.event_name != 'pull_request_target'", []string{"issue_comment"}},
		{"!(github.event_name == 'pull_request_target' || github.event_name == 'issue_comment')", nil},
		{"github.event_name == 'pull_request_target' || github.event_name == 'pull_request'", []string{"pull_request_target"}},
		{"'PULL_REQUEST' == GITHUB.EVENT_NAME", nil},
		{"github['event_name'] == 'pull_request'", nil},
		{"github.actor == 'trusted'", []string{"issue_comment", "pull_request_target"}},
		{"github.event.pull_request.head.repo.fork == false", []string{"issue_comment", "pull_request_target"}},
		{"github.event_name == 'pull_request' && success()", nil},
		{"github.event_name == 'pull_request' || failure()", []string{"issue_comment", "pull_request_target"}},
		{"(github.event_name == 'pull_request_target' && false) || github.event_name == 'issue_comment'", []string{"issue_comment"}},
		{"contains(github.event_name, 'pull_request')", []string{"issue_comment", "pull_request_target"}},
		{"false", nil},
		{"true", []string{"issue_comment", "pull_request_target"}},
		{"(github.event_name == 'pull_request' && '') == false", []string{"issue_comment", "pull_request_target"}},
	} {
		for _, wrapped := range []bool{false, true} {
			condition := tc.condition
			if wrapped {
				condition = "${{ " + condition + " }}"
			}
			for _, mode := range []string{"call", "job-write", "workflow-write", "call-write"} {
				t.Run(mode+"/"+condition, func(t *testing.T) {
					source := "on: [pull_request, pull_request_target, issue_comment]\n"
					if mode == "workflow-write" {
						source += "cache-mode: write\n"
					}
					source += "jobs:\n  test:\n    if: " + strconv.Quote(condition) + "\n"
					kind := "cache-write-untrusted"
					if mode == "call" || mode == "call-write" {
						if mode == "call-write" {
							source += "    cache-mode: write\n"
						}
						source += "    uses: example/shared/.github/workflows/build.yml@v1\n"
						if mode == "call" {
							kind = "cache-call-unrestricted"
						}
					} else {
						if mode == "job-write" {
							source += "    cache-mode: write\n"
						}
						source += cachePolicySteps
					}
					var findings []*Error
					for _, finding := range lintCachePolicy(t, source, "") {
						if finding.Kind == kind {
							findings = append(findings, finding)
						}
					}
					want := 0
					if len(tc.events) != 0 {
						want = 1
					}
					if len(findings) != want {
						t.Fatalf("want %d %s findings for %q, got %v", want, kind, condition, findings)
					}
					if want == 0 {
						return
					}
					for _, event := range []string{"issue_comment", "pull_request_target"} {
						got := strings.Contains(findings[0].Message, `"`+event+`"`)
						if got != slices.Contains(tc.events, event) {
							t.Errorf("event %q: want presence %v in %s", event, !got, findings[0].Message)
						}
					}
				})
			}
		}
	}
}

func TestCachePolicyInheritedEventUnion(t *testing.T) {
	for _, conditions := range [][]string{
		{"github.event_name == 'pull_request'", "true"},
		{"true", "github.event_name == 'pull_request'"},
		{"github.event_name == 'pull_request_target'", "github.event_name == 'issue_comment'"},
	} {
		var source strings.Builder
		source.WriteString("on: [pull_request, pull_request_target, issue_comment]\ncache-mode: write\njobs:\n")
		for i, condition := range conditions {
			source.WriteString("  job")
			source.WriteRune('a' + rune(i))
			source.WriteString(":\n    if: ")
			source.WriteString(condition)
			source.WriteString("\n")
			source.WriteString(cachePolicySteps)
		}
		var findings []*Error
		for _, finding := range lintCachePolicy(t, source.String(), "") {
			if finding.Kind == "cache-write-untrusted" {
				findings = append(findings, finding)
			}
		}
		if len(findings) != 1 {
			t.Fatalf("conditions %v: want one inherited finding, got %v", conditions, findings)
		}
		if findings[0].Line != 2 || !strings.Contains(findings[0].Message, `"issue_comment", "pull_request_target"`) {
			t.Errorf("conditions %v: want both events on workflow declaration, got %v", conditions, findings[0])
		}
	}
}

func TestCacheEventGuardUnknownSyntax(t *testing.T) {
	events := []string{"pull_request_target"}
	for _, condition := range []string{"github.event_name ==", "prefix ${{ false }}", "${{ false }} suffix", "${{ false }}${{ false }}", " ${{ false }}", "${{ false }} "} {
		got := cacheReachableEvents(events, &Job{If: &String{Value: condition}})
		if !slices.Equal(got, events) {
			t.Errorf("condition %q: want events %v, got %v", condition, events, got)
		}
	}
}
