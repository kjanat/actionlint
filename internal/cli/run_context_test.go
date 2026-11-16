package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/assembly"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
	"actionlint.kjanat.dev/v2/internal/model/sha256hex"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestWriteRunContextRendersEveryFact(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("a", 40)
	run := assembly.RunContext{
		Repositories: []assembly.RepositoryState{
			{Kind: assembly.ReadRepository, Root: "/repo", Worktree: gitcommand.Worktree{Head: head, Changes: []string{".github/workflows/ci.yml", "README.md"}}, ChangedFiles: []string{".github/workflows/ci.yml"}},
			{Kind: assembly.UnreadableRepository, Root: "/broken", ReadError: "git not found"},
			{Kind: assembly.NoRepository},
		},
		Files: []report.LintedFile{{Path: ".github/workflows/ci.yml", Digest: strings.Repeat("b", 64)}},
		Configurations: []assembly.ConfigurationState{
			{
				Project: "/repo", File: ".github/actionlint.yaml",
				Files:     []report.LintedFile{{Path: ".github/actionlint.yaml", Digest: strings.Repeat("c", 64)}, {Path: "https://example.com/base.yaml"}},
				Overrides: []string{"input config"},
				Settings:  []assembly.ChangedSetting{{Pointer: "/lint/rules/security/unpinned-reference/level", Value: `"off"`, Origin: ".github/actionlint.yaml:4"}},
			},
			{Project: "/other"},
		},
		Ignores: []assembly.IgnoreEntry{
			{Source: assembly.IgnoreSelectorFlag, Value: "unpinned-reference"},
			{Source: assembly.IgnoreRegexFlag, Value: "^SC2086"},
			{Source: assembly.IgnoreConfig, Value: "rule=shellcheck file=ci.yml", Location: ".github/actionlint.yaml:7"},
			{Source: assembly.IgnoreInline, Value: "script-injection", Location: ".github/workflows/ci.yml:9"},
		},
		Providers: []report.ProviderStatus{
			{Kind: report.PluginProvider, Name: "github.com/kjanat/actionlint-plugin-shellcheck", Version: "v0.3.1", Digest: strings.Repeat("d", 64), OnDemand: true},
			{Kind: report.ToolProvider, Name: "ruff", Inactive: "disabled in configuration"},
			{Kind: report.ToolProvider, Name: "shellcheck"},
		},
		Network: assembly.NetworkState{
			GitHubToken: "GH_TOKEN", CacheDirectory: "/cache/actionlint", Requests: 3, Failures: 1,
			MetadataCacheHits: 12, MetadataCacheMisses: 2, ResolverCacheHits: 4, MetadataAvailable: 12,
			MetadataUnavailable: []assembly.ReasonCount{{Reason: "cache-miss", Count: 2}},
		},
		Flags:    assembly.ResultFlags{Strict: true, Experimental: new(true)},
		RulesRun: 41,
		Rules:    []assembly.RuleOutcome{{Rule: "script-injection", Collected: 1, Reported: 1}, {Rule: "unpinned-reference", Collected: 2, Reported: 0}},
	}
	var out bytes.Buffer
	if err := writeRunContext(&out, run); err != nil {
		t.Fatal(err)
	}
	want := `verbose: repository: /repo at HEAD aaaaaaaaaaaa (dirty, 2 changed paths)
verbose:   uncommitted linted file: .github/workflows/ci.yml
verbose: repository: /broken (unreadable: git not found)
verbose: repository: none
verbose: file: .github/workflows/ci.yml sha256:bbbbbbbbbbbb
verbose: config: .github/actionlint.yaml for project /repo
verbose:   config file: .github/actionlint.yaml sha256:cccccccccccc
verbose:   config file: https://example.com/base.yaml (not hashed)
verbose:   config overrides: input config
verbose:   setting: /lint/rules/security/unpinned-reference/level = "off" (.github/actionlint.yaml:4)
verbose: config: none, defaults for project /other
verbose: ignore: --ignore unpinned-reference
verbose: ignore: --ignore-regex ^SC2086
verbose: ignore: config rule=shellcheck file=ci.yml (.github/actionlint.yaml:7)
verbose: ignore: inline script-injection (.github/workflows/ci.yml:9)
verbose: plugin: github.com/kjanat/actionlint-plugin-shellcheck v0.3.1 sha256:dddddddddddd on demand active
verbose: tool: ruff inactive (disabled in configuration)
verbose: tool: shellcheck active
verbose: network: online, GitHub token from GH_TOKEN, cache /cache/actionlint
verbose: network requests: 3 attempts, 1 failed; metadata cache: 12 hits, 2 misses; resolver cache hits: 4
verbose: remote metadata: 12 available, 2 unavailable (cache-miss 2)
verbose: flags: strict=true experimental=true no-ignore=false; rules run: 41
verbose: rule script-injection: 1 collected, 1 reported
verbose: rule unpinned-reference: 2 collected, 0 reported
`
	if got := out.String(); got != want {
		t.Fatalf("run context:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteRunContextDescribesEmptyRun(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := writeRunContext(&out, assembly.RunContext{Network: assembly.NetworkState{Offline: true}}); err != nil {
		t.Fatal(err)
	}
	want := `verbose: config: none, defaults
verbose: ignore: none
verbose: network: offline, GitHub token none, cache none
verbose: network requests: 0 attempts, 0 failed; metadata cache: 0 hits, 0 misses; resolver cache hits: 0
verbose: remote metadata: 0 available
verbose: flags: strict=false experimental=default no-ignore=false; rules run: 0
`
	if got := out.String(); got != want {
		t.Fatalf("run context:\n%s\nwant:\n%s", got, want)
	}
}

// TestCheckVerboseExplainsRunContext checks that a verbose check names the
// rules an ignore flag and an inline directive silenced, the ignore entries
// and their sources, the linted file's digest and the inactive tools.
func TestCheckVerboseExplainsRunContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workflow := []byte(`name: Test
on: push
permissions: {}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      # actionlint:ignore-next-line script-injection
      - run: echo "${{ github.event.head_commit.message }}"
`)
	path := filepath.Join(dir, "workflow.yml")
	if err := os.WriteFile(path, workflow, 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "actionlint.yaml")
	if err := os.WriteFile(config, []byte("lint:\n  rules:\n    security:\n      unpinned-reference: error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	command := Command{Stdout: &out, Stderr: &stderr}
	args := []string{"actionlint", "check", "--verbose", "--offline", "--config", config, "--no-tool=shellcheck", "--no-tool=ruff", "--ignore", "unpinned-reference", path}
	if status := command.MainContext(t.Context(), args); status != 0 {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, &out, &stderr)
	}
	log := stderr.String()
	for _, want := range []string{
		"verbose: file: " + path + " sha256:" + sha256hex.Digest(workflow)[:12] + "\n",
		"verbose:   config file: " + config + " sha256:",
		"verbose:   setting: /lint/rules/security/unpinned-reference/level = \"error\" (" + config + ":4)\n",
		"verbose: ignore: --ignore unpinned-reference\n",
		"verbose: ignore: inline script-injection (" + path + ":9)\n",
		"verbose: tool: ruff inactive (disabled by --no-tool or disable-tools)\n",
		"verbose: tool: shellcheck inactive (disabled by --no-tool or disable-tools)\n",
		"verbose: network: offline, GitHub token none, cache ",
		"verbose: remote metadata: 0 available, 1 unavailable (offline 1)\n",
		"verbose: flags: strict=false experimental=default no-ignore=false; rules run: ",
		"verbose: rule script-injection: 1 collected, 0 reported\n",
		"verbose: rule unpinned-reference: 1 collected, 0 reported\n",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("verbose log lacks %q:\n%s", want, log)
		}
	}
}
