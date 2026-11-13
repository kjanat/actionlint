package actions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func TestContainerRunnerNestedComposites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, guard, comment string
		want                 int
	}{
		{"identical calls coalesce", "", "", 1},
		{"guarded", "        if: runner.os == 'Linux'\n", "", 0},
		{"suppressed in action", "", " # ignore[container-runner]", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck", "ruff")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			fixturefiles.Write(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: $/inner\n")
			fixturefiles.Write(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: docker://alpine:3\n")
			fixturefiles.Write(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: docker://alpine:3"+tc.comment+"\n")
			source := "on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: actions/checkout@v6\n      - uses: $/outer\n" + tc.guard + "      - uses: $/outer\n" + tc.guard + "\npermissions: {}\n"
			workflow := fixturefiles.Write(t, root, ".github/workflows/docker.yml", source)
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, finding := range result.Diagnostics {
				if finding.Rule == "container-runner" {
					count++
					if !strings.HasSuffix(filepath.ToSlash(finding.Path), "inner/action.yml") || finding.Start.Line != 6 {
						t.Fatalf("wrong metadata source: %+v", finding)
					}
				}
			}
			if count != tc.want {
				t.Fatalf("got %d, want %d: %+v", count, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestContainerRunnerRemoteDockerMetadata(t *testing.T) {
	t.Parallel()
	// This fixture records an action that declares a Docker runtime.
	// https://github.com/actions/hello-world-docker-action/blob/8bcd8e1af3c095561f1043123848fc8b2db0f189/action.yml
	const sha = "8bcd8e1af3c095561f1043123848fc8b2db0f189"
	content, err := os.ReadFile("internal/development/tests/integration/actions/testdata/container-runner/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := actions.ParseReference("github.com", "actions/hello-world-docker-action@"+sha)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, sha, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(ref, identity, sha, actions.ObjectRoute(), "recorded-public-fixture")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifest(selection, content)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"ubuntu-latest", "windows-latest", "ubuntu-slim"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: %s\n    steps:\n      - uses: actions/hello-world-docker-action@%s\npermissions: {}\n", label, sha)
			findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "tools: {shellcheck: false, ruff: false}", checking.Options{RemoteActions: pinnedMetadataFixture{manifest}})
			count := 0
			for _, finding := range findings {
				if finding.Rule == "container-runner" {
					count++
					severity := "error"
					if label == "ubuntu-slim" {
						severity = "warning"
					}
					if finding.Severity != severity {
						t.Fatalf("severity=%s, want %s", finding.Severity, severity)
					}
				}
			}
			want := 1
			if label == "ubuntu-latest" {
				want = 0
			}
			if count != want {
				t.Fatalf("got %d, want %d: %+v", count, want, findings)
			}
		})
	}
}
