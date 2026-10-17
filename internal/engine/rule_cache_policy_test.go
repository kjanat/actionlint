package engine

import (
	"fmt"
	"slices"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

const cachePolicySteps = `    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`

func lintCachePolicy(t *testing.T, source, config string) []*findingmodel.Finding {
	t.Helper()
	request := Request{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
	var lConfig *repoconfig.Config
	var err error

	if config != "" {
		lConfig, err = repoconfig.Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	request.Sources = []SourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil, Config: lConfig}}
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	return errs
}

func TestCachePolicyRemoteCallBoundaries(t *testing.T) {
	const remote = "example/repo/.github/workflows/build.yaml@main"
	for _, tc := range []struct {
		name, uses, before, config string
		kinds                      []string
	}{
		{"uncapped", remote, "", "", []string{"cache-call-unrestricted"}},
		{"trailing exception", remote + " # actionlint:ignore cache-call-unrestricted -- reviewed remote workflow", "", "", nil},
		{"preceding exception", remote, "    # actionlint:ignore-next-line cache-call-unrestricted -- reviewed remote workflow\n", "", nil},
		{"policy disabled", remote, "", "policy: {cache-call-unrestricted: false}", nil},
		{"missing ref", "example/repo/.github/workflows/build.yaml", "", "", []string{"workflow-call"}},
		{"empty ref", "example/repo/.github/workflows/build.yaml@", "", "", []string{"workflow-call"}},
		{"empty repository", "example//.github/workflows/build.yaml@main", "", "", []string{"workflow-call"}},
		{"missing workflow path", "example/repo@main", "", "", []string{"workflow-call"}},
		{"literal expression", "${{ '" + remote + "' }}", "", "", []string{"cache-call-unrestricted"}},
		{"path expression", "example/repo/.github/workflows/${{ 'build' }}.yaml@main", "", "", []string{"expression"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: pull_request_target
jobs:
  call:
` + tc.before + "    uses: " + tc.uses + "\n"
			var kinds []string
			for _, err := range lintCachePolicy(t, source, tc.config) {
				kinds = append(kinds, err.Rule)
			}
			if !slices.Equal(kinds, tc.kinds) {
				t.Fatalf("want diagnostic kinds %v, got %v", tc.kinds, kinds)
			}
		})
	}
}

func TestCachePolicyConfig(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"cache-write-untrusted", `on: pull_request_target
cache-mode: write
jobs:
  test:
` + cachePolicySteps},
		{"cache-call-unrestricted", `on: pull_request_target
jobs:
  call:
    uses: example/repo/.github/workflows/build.yaml@main
`},
		{"cache-operation", `on: push
cache-mode: none
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/cache@v5
        with: {path: .cache, key: test}
`},
	} {
		for _, value := range []string{"true", "false", "null"} {
			t.Run(tc.name+"/"+value, func(t *testing.T) {
				errs := lintCachePolicy(t, tc.source, fmt.Sprintf("policy: {%s: %s}", tc.name, value))
				if value == "false" {
					if len(errs) != 0 {
						t.Fatal(errs)
					}
				} else if len(errs) != 1 || errs[0].Rule != tc.name {
					t.Fatal(errs)
				}
			})
		}
		for _, value := range []string{"1", "'true'", "{}", "[]"} {
			if _, err := repoconfig.Parse(fmt.Appendf(nil, "policy: {%s: %s}", tc.name, value)); err == nil {
				t.Errorf("accepted invalid %s value %s", tc.name, value)
			}
		}
	}
}
