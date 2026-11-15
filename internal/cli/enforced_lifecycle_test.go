package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

const requiredRunnerPolicy = `lint:
  lifecycle:
    policies:
      - id: required-runner
        description: Require supported runners
        match: {subjects: [runners], events: [support-ends]}
        notifications: [{starts: {on: effective-date}, severity: error}]
`

func TestCLIEnforcedLifecycleCannotBeSuppressed(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	if err := os.WriteFile("required.yml", []byte(requiredRunnerPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config, workflow string
		args                   []string
	}{
		{"lint disabled", "lint: {enabled: false}\n", arm32Workflow, nil},
		{"rule disabled", "lint: {rules: {correctness: {runner-support: off}}}\n", arm32Workflow, nil},
		{"ignore selector", "{}\n", arm32Workflow, []string{"--ignore", "runner-support"}},
		{"ignore regex", "{}\n", arm32Workflow, []string{"--ignore-regex", ".*"}},
		{"inline suppression", "{}\n", strings.Replace(arm32Workflow, "runs-on: [self-hosted, linux, ARM]", "runs-on: [self-hosted, linux, ARM] # actionlint:ignore runner-support -- reviewed", 1), nil},
	} {
		if err := os.WriteFile("project.yml", []byte(tc.config), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader(tc.workflow), Stdout: &out, Stderr: &log}
		args := []string{"actionlint", "check", "--no-tools", "--config", "project.yml", "--enforce-policy", "required.yml", "--as-of", "2026-12-08", "--output-format=json"}
		args = append(args, tc.args...)
		args = append(args, "-")
		status := command.MainContext(t.Context(), args)
		if status != 1 {
			t.Fatalf("%s: status %d, stdout %s, stderr %s", tc.name, status, &out, &log)
		}
		var report resultreport.CheckResult
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Rule != "runner-support" || diagnostic.Lifecycle == nil {
				continue
			}
			for _, policy := range diagnostic.Lifecycle.Policies {
				if policy.ID == "required-runner" && policy.Enforced && policy.Severity == "error" {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("%s: required policy finding missing: %s", tc.name, &out)
		}
	}
}

func TestCLIEnforcedLifecycleFailsClosed(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.yml"), "https://config.example/uncached-policy.yml"} {
		status, out, log := runOfflineCommand(t, acquisition, arm32Workflow, "--offline", "check", "--no-config", "--no-tools", "--enforce-policy", path, "-")
		if status != 3 {
			t.Errorf("policy %q: status %d, stdout %s, stderr %s", path, status, out, log)
		}
	}
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline policy made requests: %v", requests)
	}
}

func TestCLILifecycleAsOfConflict(t *testing.T) {
	t.Parallel()
	status, _, log := runOfflineCommand(t, plugininstall.Acquisition{}, arm32Workflow, "check", "--no-config", "--no-tools", "--as-of", "2026-01-01", "--runner-as-of", "2026-01-02", "-")
	if status == 0 || !strings.Contains(log, "--as-of and --runner-as-of") {
		t.Fatalf("status %d, stderr %s", status, log)
	}
}

func TestCLIEnforcedLifecycleWarmAndOffline(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	const policy = "https://config.example/required.yml"
	registry.SetFile(policy, []byte(requiredRunnerPolicy))
	args := []string{"--no-config", "--no-tools", "--enforce-policy", policy, "--runner-service", "github.com", "--as-of", "2026-12-08", "-"}
	status, out, log := runOfflineCommand(t, acquisition, arm32Workflow, append([]string{"cache", "warm"}, args...)...)
	if status != 0 {
		t.Fatalf("warm status %d, stdout %s, stderr %s", status, out, log)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	status, out, log = runOfflineCommand(t, acquisition, arm32Workflow, append([]string{"--offline", "check"}, args...)...)
	if status != 1 || !strings.Contains(out, "required-runner") {
		t.Fatalf("offline status %d, stdout %s, stderr %s", status, out, log)
	}
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline policy made requests: %v", requests)
	}
}
