package githubaction

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestShellcheckArguments(t *testing.T) {
	for _, value := range []string{
		`--exclude SC2086`, `[true]`, `[123]`, `null`, "[]\n---\n[]",
		`["--files-from=-"]`, `["--", "other.sh"]`, `["other.sh"]`, `["--version"]`,
		`["--list-optional"]`, `["--check-sourced"]`, `["-e"]`,
		`["-e", "--format=json"]`, `["--severity=banana"]`, `["--extended-analysis=maybe"]`,
		`["--source-path", "\u0000"]`,
	} {
		t.Run(value, func(t *testing.T) {
			args, err := shellcheckArguments(value)
			if err == nil {
				_, err = mergeShellcheckFlags(nil, args)
			}
			if err == nil {
				t.Fatalf("accepted invalid arguments: %s", value)
			}
		})
	}
	for _, value := range []string{
		`["--source-path", "scripts with 'quotes' and $dollars", "-eSC2086", "--severity=warning"]`,
		"- --source-path\n- scripts with 'quotes' and $dollars\n- -eSC2086\n- --severity=warning",
	} {
		args, err := shellcheckArguments(value)
		want := []string{"--source-path", "scripts with 'quotes' and $dollars", "-eSC2086", "--severity=warning"}
		if err != nil || !reflect.DeepEqual(args, want) {
			t.Fatalf("argument boundaries changed: %q, %v", args, err)
		}
	}
}

func TestActionShellcheckConfigPaths(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{"config with spaces": "disable=SC2086"})
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for _, config := range []string{"../outside", filepath.Join(workspace, "config with spaces"), "missing", "."} {
		req := &lintRequest{shellcheck: "shellcheck"}
		err := req.configureShellcheck(func(key string) string {
			if key == "INPUT_SHELLCHECK-CONFIG" {
				return config
			}
			return ""
		}, root, workspace)
		if err == nil {
			t.Errorf("accepted config path %q", config)
		}
	}
}

func TestMergeShellcheckFlags(t *testing.T) {
	flags, err := mergeShellcheckFlags(
		[]string{"-e2086,SC2154", "--severity=error", "--shell=sh", "--norc", "--enable=all", "-P", "with spaces"},
		[]string{"--exclude=SC2086,SC2016", "-Swarning", "--shell=bash", "--rcfile=custom config", "-fquiet", "--enable=all", "-P", "with spaces"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if flags.settings.Shell != "bash" || flags.settings.Config != actionlint.ShellcheckRCFile("custom config") {
		t.Fatalf("effective shell/config: %+v", flags.settings)
	}
	if !reflect.DeepEqual(flags.settings.Exclude, []string{"SC2086", "SC2154", "SC2016"}) {
		t.Fatalf("effective exclusions: %q", flags.settings.Exclude)
	}
	want := []string{"--severity", "warning", "--enable", "all", "--source-path", "with spaces"}
	if args := flags.arguments(); !reflect.DeepEqual(args, want) {
		t.Fatalf("effective arguments: %q; want %q", args, want)
	}
}

func TestActionShellcheckInputs(t *testing.T) {
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skipf("ShellCheck required: %s", err)
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $VALUE\n"
	for _, tc := range []struct {
		name, config, args, inherited string
		fileSettings, tools           string
		shell, script, finding        string
		rcfile                        string
		code                          int
	}{
		{name: "default remains disabled", code: 1},
		{name: "explicit disabled", config: "false", code: 1},
		{name: "discovery from working directory", config: "true"},
		{name: "discovery without dot", config: "true", rcfile: "shellcheckrc"},
		{name: "explicit file from workspace", config: "config with spaces"},
		{name: "literal arguments", args: `["-e", "SC2086", "-P", "scripts with spaces"]`},
		{name: "inherited arguments", inherited: "--exclude=SC2086"},
		{name: "input severity overrides inherited", inherited: "--severity=error", args: `["--severity=style"]`, code: 1},
		{name: "inherited format normalized", inherited: "--format=quiet", code: 1},
		{name: "input format normalized", args: `["-f", "quiet"]`, code: 1},
		{name: "config flag", args: `["--rcfile", "config with spaces"]`},
		{name: "input config overrides norc", inherited: "--norc", args: `["--rcfile", "config with spaces"]`},
		{name: "norc overrides missing config", inherited: "--rcfile=missing", args: `["--norc"]`, code: 1},
		{name: "dedicated config overrides norc", config: "config with spaces", args: `["--norc"]`},
		{name: "dedicated false overrides missing config", config: "false", args: `["--rcfile=missing"]`, code: 1},
		{name: "dedicated true overrides norc", config: "true", args: `["--norc"]`},
		{name: "shell override selects Bash", shell: "sh", script: `[[ -n "$HOME" ]]`, args: `["--shell=bash"]`},
		{name: "shell override preserves user locations", shell: "bash", script: `[[ -n "$HOME" ]]`, args: `["--shell=sh"]`, code: 1, finding: "SC3010"},
		{name: "missing config fails", config: "missing", code: 2},
		{name: "project inline config", fileSettings: "config: {disable: [SC2086]}"},
		{name: "project config directory interpolation", fileSettings: `config: "${{ configdir }}/.shellcheckrc"`},
		{name: "project workspace interpolation", fileSettings: `config: "${{ github.workspace }}/config with spaces"`},
		{name: "tools input path interpolation", tools: `shellcheck: {config: "${{ configdir }}/.shellcheckrc"}`},
		{name: "project disables tool", fileSettings: "enabled: false"},
		{name: "tools input re-enables tool", fileSettings: "enabled: false", tools: "shellcheck: {enabled: true}", code: 1},
		{name: "tools input replaces list", fileSettings: "config: {disable: [SC2086]}", tools: "shellcheck: {config: {disable: []}}", code: 1},
		{name: "tools input inline config", tools: "shellcheck: {config: {disable: [SC2086]}}"},
		{name: "flag overrides project dialect", shell: "bash", script: `[[ -n "$HOME" ]]`, fileSettings: "config: {shell: sh}", args: `["--shell=bash"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := workflow
			if tc.script != "" {
				source = strings.Replace(source, "echo $VALUE", "|\n          "+tc.script, 1)
			}
			if tc.shell != "" {
				source += "        shell: " + tc.shell + "\n"
			}
			rcfile := tc.rcfile
			if rcfile == "" {
				rcfile = ".shellcheckrc"
			}
			workspace := workspaceWith(t, map[string]string{
				".git": "", "project/workflow.yml": source,
				".github/actionlint.yaml":    "tools:\n  shellcheck:\n    " + tc.fileSettings + "\n",
				".github/.shellcheckrc":      "disable=SC2086\n",
				".github/workflows/.gitkeep": "",
				"project/" + rcfile:          "disable=SC2086\n",
				".shellcheckrc":              "disable=SC2016\n",
				"config with spaces":         "disable=SC2086\n",
			})
			t.Setenv("GITHUB_WORKSPACE", workspace)
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "workflow.yml",
				"INPUT_WORKING-DIRECTORY": "project", "INPUT_PYFLAKES": "false",
				"ACTIONLINT_SHELLCHECK_COMMAND": shellcheck,
				"INPUT_SHELLCHECK-CONFIG":       tc.config, "INPUT_SHELLCHECK-ARGS": tc.args,
				"SHELLCHECK_OPTS": tc.inherited,
				"INPUT_TOOLS":     tc.tools,
			}
			var out strings.Builder
			if code := Main(func(key string) string { return env[key] }, &out); code != tc.code {
				t.Fatalf("want exit %d, got %d: %s", tc.code, code, out.String())
			}
			finding := tc.finding
			if finding == "" {
				finding = "SC2086"
			}
			if tc.code == 1 && !strings.Contains(out.String(), finding) {
				t.Fatalf("expected ShellCheck finding: %s", out.String())
			}
			if strings.Contains(out.String(), "SC3040") || strings.Contains(out.String(), ":0:") {
				t.Fatalf("reported synthetic startup line: %s", out.String())
			}
		})
	}
}
