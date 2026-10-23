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
	envelope := struct {
		Content   json.RawMessage  `json:"content"`
		Scope     string           `json:"scope"`
		Freshness runner.Freshness `json:"freshness"`
		Complete  bool             `json:"complete"`
	}{Content: catalogue, Scope: "fixture", Freshness: runner.Fresh, Complete: true}
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
	data := []byte(`{
		"scope": "fixture", "freshness": "fresh", "complete": true,
		"content": {"runners":[{"name":"desktop","labels":["desktop-pool"],"os":"windows","default_shell":"pwsh"}]}
	}`)
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

func TestRunnerSnapshotContent(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
	}{
		{"object", `{"runners":[]}`, `{"runners":[]}`},
		{"array", `["pool"]`, `["pool"]`},
		{"text", `"opaque\nprovider data"`, "opaque\nprovider data"},
		{"null", `null`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "snapshot.json")
			data := `{"content":` + tc.value + `,"scope":"org/repo","obtained_at":"2026-10-23T00:00:00Z","freshness":"fresh","complete":true}`
			if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			options, err := (checkInvocation{RunnerSnapshot: file}).runners()
			want := runner.Snapshot{Content: tc.want, Scope: "org/repo", ObtainedAt: "2026-10-23T00:00:00Z", Freshness: runner.Fresh, Complete: true}
			if err != nil || options.Snapshot != want {
				t.Fatalf("snapshot=%+v error=%v; want %+v", options.Snapshot, err, want)
			}
		})
	}
}
