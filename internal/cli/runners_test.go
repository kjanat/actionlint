package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/runner"
)

func TestRunnerProviderCLI(t *testing.T) {
	artifact, err := filepath.Abs("internal/plugins/component/testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := os.ReadFile("testdata/runner-provider/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	envelope := runner.Snapshot{Content: string(catalogue), Scope: "fixture", Freshness: runner.Fresh, Complete: true}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("runners.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		target string
		status int
	}{{"buildkite", 0}, {"github", 1}} {
		var output bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: buildkite-windows\n    steps: [{run: echo ok}]\n"), Stdout: &output, Stderr: &output}
		status := command.MainContext(t.Context(), []string{"actionlint", "--no-config", "--no-shellcheck", "--no-ruff", "--plugin", artifact, "--runner-target", tc.target, "--runner-scope", "fixture", "--runner-snapshot", "runners.json", "-"})
		if status != tc.status {
			t.Fatalf("%s: status %d: %s", tc.target, status, &output)
		}
	}
}

func TestRunnerSnapshotValidation(t *testing.T) {
	for _, content := range []string{`{"freshness":"current"}`, `{"freshness":"fresh","typo":true}`, `null`, `{`} {
		file := filepath.Join(t.TempDir(), "snapshot.json")
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := (checkInvocation{RunnerSnapshot: file}).runners(); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}

func TestGitHubRunnerSnapshotCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	envelope := runner.Snapshot{
		Scope: "fixture", Freshness: runner.Fresh, Complete: true,
		Content: `{"runners":[{"name":"desktop","labels":["desktop-pool"],"os":"windows","default_shell":"pwsh"}]}`,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("runners.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		provider, scope string
		status          int
		message         string
	}{
		{"snapshot", "fixture", 0, ""},
		{"github", "fixture", 1, "runner-label"},
		{"snapshot", "other", 3, "scope does not match"},
	} {
		var output bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: desktop-pool\n    steps: [{run: echo ok}]\n"), Stdout: &output, Stderr: &output}
		status := command.MainContext(t.Context(), []string{"actionlint", "--no-config", "--no-shellcheck", "--no-ruff", "--runner-provider", tc.provider, "--runner-scope", tc.scope, "--runner-snapshot", "runners.json", "-"})
		if status != tc.status || !strings.Contains(output.String(), tc.message) {
			t.Fatalf("provider=%s scope=%s status=%d output=%s", tc.provider, tc.scope, status, &output)
		}
	}
}
