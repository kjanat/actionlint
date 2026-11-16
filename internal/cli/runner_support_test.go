package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

const arm32Workflow = "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: [self-hosted, linux, ARM]\n    steps: [{run: echo ok}]\n"

func TestRunnerSupportSeverityAndFiltering(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	for _, tc := range []struct {
		name, setting, severity string
		ignore                  bool
	}{
		{name: "default warning", severity: "warning"},
		{name: "configured error", setting: "error", severity: "error"},
		{name: "disabled rule", setting: "off"},
		{name: "ignored rule", ignore: true},
	} {
		config := "plugins = []\n"
		if tc.setting != "" {
			config += "[lint.rules.correctness]\nrunner-support = '" + tc.setting + "'\n"
		}
		if err := os.WriteFile("actionlint.toml", []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		var output, log bytes.Buffer
		command := Command{Stdin: strings.NewReader(arm32Workflow), Stdout: &output, Stderr: &log}
		args := []string{"actionlint", "check", "--config", "actionlint.toml", "--no-tools", "--runner-as-of", "2026-12-08", "--output-format=json", "-"}
		if tc.ignore {
			args = append(args, "--ignore", "runner-support")
		}
		if status := command.MainContext(t.Context(), args); status > 1 {
			t.Fatalf("%s: check failed with status %d: %s\n%s", tc.name, status, &log, &output)
		}
		var report report.CheckResult
		if err := json.Unmarshal(output.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Rule != "runner-support" {
				continue
			}
			found++
			if string(diagnostic.Severity) != tc.severity || diagnostic.Start.Line != 5 {
				t.Errorf("expected severity %q on runs-on line 5, got %+v", tc.severity, diagnostic)
			}
		}
		want := 0
		if tc.severity != "" {
			want = 1
		}
		if found != want {
			t.Errorf("%s: got %d runner-support diagnostics, want %d: %s", tc.name, found, want, &output)
		}
	}
}

func TestRunnerSupportServiceAndDateCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_SERVER_URL", "")
	for _, tc := range []struct {
		name, service, version, date string
		finding                      bool
	}{
		{"before change", "github.com", "", "2026-09-22", false},
		{"on change", "github.com", "", "2026-09-23", true},
		{"after change", "github.com", "", "2026-12-08", true},
		{"data residency", "github-data-residency", "", "2026-12-08", true},
		{"enterprise", "ghes", "", "2026-12-08", false},
		{"enterprise version", "ghes", "3.19", "2026-12-08", false},
		{"unknown service", "unknown", "", "2026-12-08", false},
	} {
		var output bytes.Buffer
		command := Command{Stdin: strings.NewReader(arm32Workflow), Stdout: &output, Stderr: &output}
		args := []string{"actionlint", "check", "--no-config", "--no-tools", "--runner-service", tc.service, "--runner-as-of", tc.date, "--color=never", "-"}
		if tc.version != "" {
			args = append(args, "--runner-service-version", tc.version)
		}
		status := command.MainContext(t.Context(), args)
		if status >= 2 {
			t.Fatalf("check could not complete, status %d: %s", status, &output)
		}
		found := strings.Contains(output.String(), "runner-support")
		if found != tc.finding {
			t.Fatalf("%s: support finding=%t, want %t: %s", tc.name, found, tc.finding, &output)
		}
		if found && !strings.Contains(output.String(), "2026-09-23") {
			t.Fatalf("missing effective date: %s", &output)
		}
	}
}

func TestRunnerServiceConfigurationAndFlagPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	if err := os.WriteFile("actionlint.toml", []byte("plugins = []\n[repository]\nservice = {kind = 'ghes', version = '3.19'}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		service string
		finding bool
	}{{"", false}, {"github.com", true}, {"unknown", false}} {
		var output bytes.Buffer
		command := Command{Stdin: strings.NewReader(arm32Workflow), Stdout: &output, Stderr: &output}
		args := []string{"actionlint", "check", "--config", "actionlint.toml", "--no-tools", "--runner-as-of", "2026-12-08", "--color=never", "-"}
		if tc.service != "" {
			args = append(args, "--runner-service", tc.service)
		}
		status := command.MainContext(t.Context(), args)
		if status >= 2 || strings.Contains(output.String(), "runner-support") != tc.finding {
			t.Fatalf("service %q: status=%d want finding=%t: %s", tc.service, status, tc.finding, &output)
		}
	}
}

func TestRunnerSupportComponentCLI(t *testing.T) {
	artifact, err := filepath.Abs("internal/plugins/pluginexecution/component/testdata/plugins/runner-support-0.3.0.wasm")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_SERVER_URL", "")
	for _, tc := range []struct {
		service, version, date, message string
	}{
		{"github.com", "", "2026-11-11", ""},
		{"github.com", "", "2026-11-12", "will end by 2026-12-08"},
		{"github.com", "", "2026-12-08", "has ended as of 2026-12-08"},
		{"ghes", "3.19", "2026-12-08", "has ended as of 2026-12-08"},
		{"ghes", "3.20", "2026-12-08", ""},
		{"ghes", "", "2026-12-08", ""},
	} {
		var output bytes.Buffer
		command := Command{Stdin: strings.NewReader(arm32Workflow), Stdout: &output, Stderr: &output}
		args := []string{"actionlint", "check", "--no-config", "--no-tools", "--plugin", artifact, "--runner-provider", "runner-support-fixture", "--runner-service", tc.service, "--runner-as-of", tc.date, "--color=never", "-"}
		if tc.version != "" {
			args = append(args, "--runner-service-version", tc.version)
		}
		status := command.MainContext(t.Context(), args)
		if status >= 2 {
			t.Fatalf("component check failed, status %d: %s", status, &output)
		}
		if tc.message == "" {
			if strings.Contains(output.String(), "runner-support") {
				t.Fatalf("unexpected support finding: %s", &output)
			}
		} else if !strings.Contains(output.String(), tc.message) {
			t.Fatalf("missing support message %q: %s", tc.message, &output)
		}
	}
}
