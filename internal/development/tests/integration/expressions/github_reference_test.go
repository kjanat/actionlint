package expressions_test

import (
	"strings"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
)

// Whole-workflow examples from GitHub's expression reference, checked 2026-11-04:
// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#fromjson
func TestGitHubReferenceWorkflows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, source string }{
		{"literals", `on: push
env:
  myNull: ${{ null }}
  myBoolean: ${{ false }}
  myIntegerNumber: ${{ 711 }}
  myFloatNumber: ${{ -9.2 }}
  myHexNumber: ${{ 0xff }}
  myExponentialNumber: ${{ -2.99e-2 }}
  myString: Mona the Octocat
  myStringInBraces: ${{ 'It''s open source!' }}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`},
		{"matrix passed between jobs", `name: build
on: push
jobs:
  job1:
    runs-on: ubuntu-latest
    outputs:
      matrix: ${{ steps.set-matrix.outputs.matrix }}
    steps:
      - id: set-matrix
        run: echo "matrix={\"include\":[{\"project\":\"foo\",\"config\":\"Debug\"},{\"project\":\"bar\",\"config\":\"Release\"}]}" >> $GITHUB_OUTPUT
  job2:
    needs: job1
    runs-on: ubuntu-latest
    strategy:
      matrix: ${{ fromJSON(needs.job1.outputs.matrix) }}
    steps:
      - run: echo "Matrix - Project ${{ matrix.project }}, Config ${{ matrix.config }}"
permissions: {}
`},
		{"environment conversions", `name: print
on: push
env:
  continue: true
  time: 3
jobs:
  job1:
    runs-on: ubuntu-latest
    steps:
      - continue-on-error: ${{ fromJSON(env.continue) }}
        timeout-minutes: ${{ fromJSON(env.time) }}
        run: echo ...
permissions: {}
`},
		{"event selection", `on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - if: contains(fromJSON('["push", "pull_request"]'), github.event_name)
        run: echo ok
permissions: {}
`},
		{"status conditions", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Failing step
        id: demo
        run: exit 1
      - if: ${{ failure() && steps.demo.conclusion == 'failure' }}
        run: echo failed
      - if: ${{ success() }}
        run: echo succeeded
      - if: ${{ always() }}
        run: echo cleanup
      - if: ${{ cancelled() }}
        run: echo cancelled
      - if: ${{ !cancelled() }}
        run: echo active
permissions: {}
`},
		{"multiline case", `on: push
env:
  MY_ENV_VAR: |-
    ${{ case(
      github.ref == 'refs/heads/main', 'production',
      github.ref == 'refs/heads/staging', 'staging',
      startsWith(github.ref, 'refs/heads/feature/'), 'development',
      'unknown'
    ) }}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, finding := range testutil.WorkflowDiagnostics(t, tc.source, "") {
				// The published matrix example interpolates runtime values into a
				// shell command. That security finding is separate from expression validity.
				if tc.name == "matrix passed between jobs" && finding.Rule == "script-interpolation" {
					continue
				}
				t.Errorf("documented workflow rejected: %s: %s", finding.Rule, finding.Message)
			}
		})
	}
}

func TestGitHubReferenceMatrixMembers(t *testing.T) {
	t.Parallel()
	for _, member := range []string{"project", "config", "missing"} {
		t.Run(member, func(t *testing.T) {
			t.Parallel()
			workflow := `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix: ${{ fromJSON('{"include":[{"project":"foo","config":"Debug"},{"project":"bar","config":"Release"}]}') }}
    name: ${{ matrix.` + member + ` }}
    steps:
      - run: echo ok
` + "\npermissions: {}\n"
			findings := testutil.WorkflowDiagnostics(t, workflow, "")
			if member != "missing" {
				if len(findings) != 0 {
					t.Fatalf("known matrix member rejected: %v", findings)
				}
			} else if len(findings) != 1 || findings[0].Rule != "expression-reference" || !strings.Contains(findings[0].Message, "missing") {
				t.Fatalf("missing matrix member not diagnosed: %v", findings)
			}
		})
	}
}
