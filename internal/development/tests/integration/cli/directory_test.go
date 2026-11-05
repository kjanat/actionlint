package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestChangeDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll("project/nested", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("project/nested/ci.yml", []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("project/nested/settings.yml", []byte("lint: {rules: {correctness: {expression-reference: off}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", "project/nested", "ci.yml"},
		{"-Cproject", "-C", "nested", "check", "ci.yml"},
		{"check", "-C", "project/nested", "ci.yml"},
		{"--directory=project/nested", "ci.yml"},
	} {
		got := testRunCommand("", append(args, "--no-config")...)
		if got.Status != 1 || !strings.Contains(got.Stdout, "ci.yml:6:") || strings.Contains(got.Stdout, "project/nested") {
			t.Fatalf("directory %v: %+v", args, got)
		}
		cwd, err := os.Getwd()
		if err != nil || cwd != root {
			t.Fatalf("directory leaked: %q, %v", cwd, err)
		}
	}
	got := testRunCommand("", "-C", "project/nested", "--config", "settings.yml", "ci.yml")
	if got.Status != 0 {
		t.Fatalf("configuration did not resolve from -C: %+v", got)
	}
	got = testRunCommand("", "-C", "project", "-C", "absent", "check")
	if got.Status != 2 || !strings.Contains(got.Stderr, "cannot change directory") {
		t.Fatalf("invalid directory: %+v", got)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != root {
		t.Fatalf("failed invocation leaked directory: %q, %v", cwd, err)
	}
	if err := os.WriteFile("project/nested/check", []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	got = testRunCommand("", "-C", "project/nested", "check", "--no-config")
	if got.Status != 1 || !strings.Contains(got.Stdout, "check:6:") {
		t.Fatalf("command/file routing ignored -C: %+v", got)
	}
	got = testRunCommand("", "-C")
	if got.Status != 2 || !strings.Contains(got.Stderr, "needs an argument") {
		t.Fatalf("missing directory value: %+v", got)
	}
	got = testRunCommand("", "-C", "absent", "--json")
	if got.Status != 2 || !json.Valid([]byte(got.Stderr)) {
		t.Fatalf("directory error ignored JSON output: %+v", got)
	}
}

func TestChangeDirectoryBareInvocation(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("outside", 0o700); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	command := cli.Command{Stdout: &out, Stderr: &stderr}
	if status := command.Main([]string{"actionlint", "-C", "outside"}); status != 0 || !strings.Contains(out.String(), "Usage:") || stderr.Len() != 0 {
		t.Fatalf("bare -C outside repository: status=%d stdout=%s stderr=%s", status, &out, &stderr)
	}
}

func TestExcludedFilesDoNotReportSuccess(t *testing.T) {
	commandTestRepo(t)
	if err := os.WriteFile(".github/actionlint.yaml", []byte("files: {excludes: ['**']}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommandDefaults("", "check")
	if got.Status != 3 || strings.Contains(got.Stderr, "All clear") || !strings.Contains(got.Stderr, "no workflow or action files were selected") {
		t.Fatalf("empty analysis reported success: %+v", got)
	}
}

func TestDirectoryExclusionsAndExplicitFileOverride(t *testing.T) {
	commandTestRepo(t)
	if err := os.MkdirAll(filepath.Join("testdata", ".github", "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join("testdata", ".github", "workflows", "ci.yml")
	if err := os.WriteFile(file, []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{
		"excludes: ['testdata/**']",
		"includes: ['**', '!testdata/**']",
		"includes: ['.github/workflows/**', '!testdata/**']",
	} {
		t.Run(scope, func(t *testing.T) {
			if err := os.WriteFile(".github/actionlint.yaml", []byte("files: {"+scope+"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got := testRunCommandDefaults("", "-o", "pretty", "testdata")
			if got.Status != 3 || strings.Contains(got.Stderr, "All clear") || !strings.Contains(got.Stderr, "no workflow or action files were selected") {
				t.Fatalf("directory input overrode configured exclusions: %+v", got)
			}
			got = testRunCommandDefaults("", "-o", "pretty", file)
			if got.Status != 1 || !strings.Contains(got.Stdout, file) || !strings.Contains(got.Stderr, "Checked 1 file") {
				t.Fatalf("literal file did not override file filters: %+v", got)
			}
			got = testRunCommandDefaults("", "-o", "pretty", ".")
			if got.Status != 1 || strings.Contains(got.Stdout, "testdata") || !strings.Contains(got.Stderr, "Checked 1 file") {
				t.Fatalf("broad discovery ignored the configured exclusion: %+v", got)
			}
		})
	}
}
