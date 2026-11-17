package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCompositeLanguageLifecycleCannotBypassRequiredPolicy(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	const required = "lint:\n  lifecycle:\n    policies:\n      - id: required-language-runtime\n        description: Require supported Node.js releases\n        match: {subjects: [language-runtimes], events: [support-ends]}\n        notifications: [{starts: {on: effective-date}, severity: error}]\n"
	const composite = "name: Legacy\ndescription: Legacy\nruns:\n  using: composite\n  steps:\n    - uses: actions/setup-node@v6\n      with:\n        node-version: 12\n"
	for path, content := range map[string]string{
		"required.yml": required,
		"project.yml":  "lint: {enabled: false}\n",
		"action.yml":   composite,
		"workflow.yml": "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{"action.yml", "workflow.yml"} {
		for _, date := range []string{"2022-04-29", "2022-04-30"} {
			var out, log bytes.Buffer
			command := Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log}
			status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", "project.yml", "--enforce-policy", "required.yml", "--runner-service", "github.com", "--as-of", date, "--offline", "--no-tools", "--json", "--summary=false", input})
			if status > 1 {
				t.Fatalf("%s %s: status=%d stdout=%s stderr=%s", input, date, status, &out, &log)
			}
			var report report.CheckResult
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, d := range report.Diagnostics {
				if d.Rule != "runtime-lifecycle" {
					continue
				}
				count++
				if d.Severity != "error" || d.Lifecycle == nil || d.Lifecycle.AsOf != date || d.Lifecycle.EffectiveOn != "2022-04-30" || d.Start.Line != 8 || !strings.HasSuffix(d.Path, "action.yml") || len(d.Lifecycle.Policies) != 1 || !d.Lifecycle.Policies[0].Enforced {
					t.Fatalf("incorrect composite provenance: %+v", d)
				}
			}
			want := 0
			if date == "2022-04-30" {
				want = 1
			}
			if count != want || (status == 1) != (want == 1) {
				t.Fatalf("%s %s: status=%d count=%d want=%d output=%s", input, date, status, count, want, &out)
			}
		}
	}
}

func TestStandaloneActionLifecycleUsesCapturedContext(t *testing.T) {
	t.Chdir(t.TempDir())
	const required = "lint:\n  lifecycle:\n    policies:\n      - id: required-action-runtime\n        description: Require supported action runtimes\n        match: {subjects: [action-runtimes], events: [support-ends]}\n        notifications: [{starts: {on: effective-date}, severity: error}]\n"
	for path, content := range map[string]string{
		"required.yml": required,
		"project.yml":  "lint: {enabled: false}\n",
		"action.yml":   "name: Example\ndescription: Example\nruns:\n  using: node20\n  main: index.js\n",
		"index.js":     "",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		service, date string
		want          int
	}{
		{"github.com", "2026-09-22", 0},
		{"github.com", "2026-09-23", 1},
		{"github-data-residency", "2026-09-23", 1},
		{"ghes", "2026-09-23", 0},
	} {
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", "project.yml", "--enforce-policy", "required.yml", "--runner-service", tc.service, "--runner-as-of", tc.date, "--offline", "--no-tools", "--json", "--summary=false", "action.yml"})
		if status > 1 {
			t.Fatalf("%s %s: status=%d stderr=%s stdout=%s", tc.service, tc.date, status, &log, &out)
		}
		var report report.CheckResult
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, d := range report.Diagnostics {
			if d.Lifecycle == nil || d.Lifecycle.Subject != "action-runtimes" {
				continue
			}
			found++
			if d.Severity != "error" || d.Lifecycle.AsOf != tc.date || d.Lifecycle.EffectiveOn != "2026-09-23" || len(d.Lifecycle.Policies) != 1 || !d.Lifecycle.Policies[0].Enforced {
				t.Fatalf("lost context or authority: %+v", d)
			}
		}
		if found != tc.want || (status == 1) != (tc.want == 1) {
			t.Fatalf("%s %s: status=%d count=%d want=%d output=%s", tc.service, tc.date, status, found, tc.want, &out)
		}
	}
}
