package workflow_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// GitHub requires Linux with Docker for managed containers. ubuntu-slim runs
// unprivileged and does not support Docker-in-Docker.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idcontainer
// https://docs.github.com/en/actions/reference/runners/github-hosted-runners#single-cpu-runners
func TestContainerRunner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, runner, job, steps string
		want                     int
	}{
		{"Windows action", "windows-latest", "", "      - uses: docker://alpine:3\n", 1},
		{"macOS action", "macos-latest", "", "      - uses: docker://alpine:3\n", 1},
		{"slim action", "ubuntu-slim", "", "      - uses: docker://alpine:3\n", 1},
		{"Linux action", "ubuntu-latest", "", "      - uses: docker://alpine:3\n", 0},
		{"unknown runner", "${{ inputs.runner }}", "", "      - uses: docker://alpine:3\n", 0},
		{"Windows job container", "windows-latest", "    container: alpine:3\n", "      - run: echo ok\n", 1},
		{"Windows service", "windows-latest", "    services: {db: {image: postgres:17}}\n", "      - run: echo ok\n", 1},
		{"expression services", "windows-latest", "    services: ${{ fromJSON('{\"db\":{\"image\":\"postgres:17\"}}') }}\n", "      - run: echo ok\n", 1},
		{"empty service", "windows-latest", "    services: {db: {image: ''}}\n", "      - run: echo ok\n", 0},
		{"OS guard", "windows-latest", "", "      - if: runner.os == 'Linux'\n        uses: docker://alpine:3\n", 0},
		{"status and OS guard", "windows-latest", "", "      - if: success() && runner.os == 'Linux'\n        uses: docker://alpine:3\n", 0},
		{"skipped job", "windows-latest", "    if: false\n    container: alpine:3\n", "      - run: echo ok\n", 0},
		{"Docker CLI is independent", "windows-latest", "", "      - run: docker --host tcp://builder.example:2376 run alpine:3\n", 0},
		{"mixed matrix", "${{ matrix.os }}", "    strategy: {matrix: {os: [ubuntu-latest, windows-latest]}}\n", "      - uses: docker://alpine:3\n", 1},
		{"distinct limitations", "${{ matrix.os }}", "    strategy: {matrix: {os: [ubuntu-slim, windows-latest]}}\n", "      - uses: docker://alpine:3\n", 2},
		{"guarded matrix", "${{ matrix.os }}", "    strategy: {matrix: {os: [ubuntu-latest, windows-latest]}}\n", "      - if: matrix.os == 'ubuntu-latest'\n        uses: docker://alpine:3\n", 0},
		{"fixed runner matrix guard", "windows-latest", "    strategy: {matrix: {enabled: [false]}}\n", "      - if: matrix.enabled\n        uses: docker://alpine:3\n", 0},
		{"fixed runner matrix container", "windows-latest", "    strategy: {matrix: {image: [alpine]}}\n    container: ${{ matrix.image }}\n", "      - run: echo ok\n", 1},
		{"empty matrix", "${{ matrix.os }}", "    strategy: {matrix: {os: [windows-latest], exclude: [{os: windows-latest}]}}\n", "      - uses: docker://alpine:3\n", 0},
		{"other runner properties unknown", "windows-latest", "", "      - if: runner.arch != 'X64'\n        uses: docker://alpine:3\n", 1},
		{"function OS guard", "windows-latest", "", "      - if: contains(fromJSON('[\"Linux\"]'), runner.os)\n        uses: docker://alpine:3\n", 0},
		{"unrelated unknown matrix", "windows-latest", "    strategy: {matrix: {item: '${{ fromJSON(vars.ITEMS) }}'}}\n", "      - uses: docker://alpine:3\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  test:\n    runs-on: " + tc.runner + "\n" + tc.job + "    steps:\n" + tc.steps
			findings := testutil.WorkflowDiagnostics(t, source, "tools: {shellcheck: false, ruff: false}")
			count := 0
			for _, finding := range findings {
				if finding.Rule == "container-runner" {
					count++
					if finding.Start.Line < 4 || finding.Start.Column < 1 {
						t.Fatalf("missing source location: %+v", finding)
					}
				}
			}
			if count != tc.want {
				t.Fatalf("container-runner findings=%d, want %d: %+v", count, tc.want, findings)
			}
		})
	}
}

func TestContainerRunnerSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config, comment, severity, runner string
		strict                                  bool
	}{
		{"default", "", "", "error", "windows-latest", false},
		{"on", "lint: {rules: {preset: none, correctness: {container-runner: on}}}", "", "error", "windows-latest", false},
		{"warning", "lint: {rules: {preset: none, correctness: {container-runner: warn}}}", "", "warning", "windows-latest", false},
		{"off", "lint: {rules: {correctness: {container-runner: off}}}", "", "", "windows-latest", false},
		{"strict", "", "", "error", "windows-latest", true},
		{"inline", "", " # ignore[container-runner]", "", "windows-latest", false},
		{"override", "overrides: [{includes: ['test.yaml'], lint: {rules: {correctness: {container-runner: off}}}}]", "", "", "windows-latest", false},
		{"slim default", "", "", "warning", "ubuntu-slim", false},
		{"slim on", "lint: {rules: {preset: none, correctness: {container-runner: on}}}", "", "warning", "ubuntu-slim", false},
		{"slim strict", "", "", "warning", "ubuntu-slim", true},
		{"slim explicit error", "lint: {rules: {correctness: {container-runner: error}}}", "", "error", "ubuntu-slim", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  test:\n    runs-on: " + tc.runner + "\n    steps:\n      - uses: docker://alpine:3" + tc.comment + "\n"
			findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "tools: {shellcheck: false, ruff: false}\n"+tc.config, checking.Options{Presets: repoconfig.Presets{Strict: tc.strict}})
			count := 0
			for _, finding := range findings {
				if finding.Rule != "container-runner" {
					continue
				}
				count++
				if finding.Severity != tc.severity || finding.Start.Line != 6 || finding.End.Column <= finding.Start.Column {
					t.Fatalf("wrong severity or range: %+v", finding)
				}
			}
			want := 1
			if tc.severity == "" {
				want = 0
			}
			if count != want {
				t.Fatalf("got %d, want %d: %+v", count, want, findings)
			}
		})
	}
}
