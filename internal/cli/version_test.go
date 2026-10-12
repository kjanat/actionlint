package cli

import (
	"bytes"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestDocumentationRef(t *testing.T) {
	const revision = "02d216da011da64ff17a48a9d2adcfb54131b765"
	sourceBuild := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.modified", Value: "true"},
	}}
	for _, tc := range []struct {
		name, version string
		info          *debug.BuildInfo
		want          string
	}{
		{"release stamp", "1.16.1", nil, "v1.16.1"},
		{"module release", "v1.16.1", nil, "v1.16.1"},
		{"release stamp takes precedence", "1.16.1", sourceBuild, "v1.16.1"},
		{"prerelease", "1.17.0-rc.1", nil, "v1.17.0-rc.1"},
		{"module prerelease", "v1.17.0-beta.2", nil, "v1.17.0-beta.2"},
		{"development checkout", "(devel)", sourceBuild, revision},
		{"unknown version with revision", "unknown", sourceBuild, revision},
		{"dirty tag", "v1.16.1+dirty", sourceBuild, revision},
		{"git describe dirty tag", "v1.16.1-dirty", sourceBuild, revision},
		{"pseudo-version full revision", "v1.16.2-0.20260910203408-02d216da011d", sourceBuild, revision},
		{"dirty pseudo-version full revision", "v1.16.2-0.20260910203408-02d216da011d+dirty", sourceBuild, revision},
		{"go install commit", "v1.16.2-0.20260910203408-02d216da011d", nil, "02d216da011d"},
		{"dirty pseudo-version alone", "v1.16.2-0.20260910203408-02d216da011d+dirty", nil, "02d216da011d"},
		{"pseudo-version without prior tag", "v0.0.0-20260910203408-02d216da011d", nil, "02d216da011d"},
		{"pseudo-version after prerelease", "v1.17.0-rc.1.0.20260910203408-02d216da011d", nil, "02d216da011d"},
		{"pseudo-version with metadata", "v2.0.1-0.20260910203408-02d216da011d+incompatible", nil, "02d216da011d"},
		{"git describe full revision", "v1.16.1-12-g02d216d", sourceBuild, revision},
		{"git describe alone", "v1.16.1-12-g02d216d", nil, "02d216d"},
		{"dirty git describe alone", "v1.16.1-12-g02d216d-dirty", nil, "02d216d"},
		{"unstamped go run", "(devel)", nil, "HEAD"},
		{"empty build info", "unknown", &debug.BuildInfo{}, "HEAD"},
		{"no revision setting", "(devel)", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}}}, "HEAD"},
		{"empty revision", "(devel)", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision"}}}, "HEAD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := documentationRef(tc.version, tc.info); got != tc.want {
				t.Fatalf("documentation ref = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVersionShorthand(t *testing.T) {
	for _, tail := range [][]string{nil, {"--json"}} {
		want := testRunCommand("", append([]string{"--version"}, tail...)...)
		got := testRunCommand("", append([]string{"-V"}, tail...)...)
		if want != got || got.Status != 0 {
			t.Fatalf("-V differs from --version: %+v / %+v", got, want)
		}
	}
	if got := testRunCommand(commandGoodWorkflow, "-v", "-"); got.Status != 0 || got.Stdout != "" || !strings.Contains(got.Stderr, "verbose: Linting") {
		t.Fatalf("-v stopped enabling verbose checks: %+v", got)
	}
	if got := testRunCommand(commandGoodWorkflow, "-V=false", "-"); got != (commandTranscript{}) {
		t.Fatalf("-V=false did not run the check: %+v", got)
	}
}

func TestCommandVersionNamesTheModule(t *testing.T) {
	var output bytes.Buffer
	cmd := Command{Stdin: os.Stdin, Stdout: &output, Stderr: &output}

	if status := cmd.Main([]string{"actionlint", "-version"}); status != actionlint.ExitStatusSuccessNoProblem {
		t.Fatal("exit status should be 0 but got", status)
	}

	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("version output should have 3 lines but has %d: %q", len(lines), output.String())
	}
	if !strings.HasPrefix(lines[0], "actionlint.kjanat.dev ") {
		t.Errorf("first line should start with the module path: %q", lines[0])
	}
}
