package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/remotescan"
)

func TestCLIInvalidOptions(t *testing.T) {
	for _, args := range [][]string{{}, {"owner/repo@"}, {"--repository", "owner/repo", "other/repo"}, {"--ref", "../escape", "owner/repo"}, {"--server-url", "http://github.com", "owner/repo"}, {"--max-bytes", "-1", "owner/repo"}, {"--unknown"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), args, &out, &stderr, func(string) string { return "" })
		if code != 2 || stderr.Len() == 0 {
			t.Fatalf("args %v: code %d, stderr %q", args, code, &stderr)
		}
	}
}

func TestCLISnapshotExitCodes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Git fixture: %v %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "--template=", "--initial-branch=main")
	git("remote", "add", "origin", "https://github.com/owner/repo.git")
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		content string
		code    int
	}{
		{"on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n", 0},
		{"on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ unknown.value }}\n", 1},
	} {
		if err := os.WriteFile(filepath.Join(dir, ".github", "workflows", "test.yml"), []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Fixture")
		sha := git("rev-parse", "HEAD")
		var out, stderr bytes.Buffer
		code := run(context.Background(), []string{"--local", dir, "owner/repo@" + sha}, &out, &stderr, func(string) string { return "" })
		if code != tc.code {
			t.Fatalf("expected %d, got %d: %s %s", tc.code, code, &out, &stderr)
		}
		var result remotescan.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Source.SHA != sha || result.Source.Transport != "local" {
			t.Fatalf("incorrect source: %+v", result)
		}
	}
}

func TestCLIExplicitRefOverridesActionsAndPositionalRef(t *testing.T) {
	var out, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := map[string]string{"GITHUB_REPOSITORY": "owner/repo", "GITHUB_SHA": strings.Repeat("a", 40)}
	code := run(ctx, []string{"--ref", "release", "owner/repo@main"}, &out, &stderr, func(key string) string { return env[key] })
	if code != 3 || !strings.Contains(out.String(), `"requested_ref":"release"`) {
		t.Fatalf("code %d, output %s, errors %s", code, &out, &stderr)
	}
}

func TestCLIHelp(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), []string{"--help"}, &out, &out, func(string) string { return "" }); code != 0 || !strings.Contains(out.String(), "OWNER/REPO") {
		t.Fatalf("help code %d, output %s", code, &out)
	}
}
