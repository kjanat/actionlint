package githubaction

import (
	"encoding/json"
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
		`["--list-optional"]`, `["--check-sourced=false"]`, `["-e"]`,
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
	workspace := workspaceWith(t, map[string]string{"config with spaces": "disable=SC2086", "rc": "disable=SC2086"})
	outside := workspaceWith(t, map[string]string{"shared-rc": "disable=SC2086"})
	relative, err := filepath.Rel(workspace, filepath.Join(outside, "shared-rc"))
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{"rc", "config with spaces", filepath.Join(workspace, "config with spaces"), relative, filepath.Join(outside, "shared-rc")} {
		for _, name := range []string{"INPUT_SHELLCHECK-RC", "INPUT_SHELLCHECK-ARGS", "SHELLCHECK_OPTS"} {
			t.Run(name+"/"+config, func(t *testing.T) {
				if name == "SHELLCHECK_OPTS" && strings.Contains(config, " ") {
					t.Skip("SHELLCHECK_OPTS splits spaces without quote expansion")
				}
				req := &lintRequest{shellcheck: "shellcheck", workingDir: workspace}
				err := req.configureShellcheck(func(key string) string {
					if key != name {
						return ""
					}
					switch name {
					case "INPUT_SHELLCHECK-ARGS":
						value, err := json.Marshal([]string{"--rcfile", config})
						if err != nil {
							t.Fatal(err)
						}
						return string(value)
					case "SHELLCHECK_OPTS":
						return "--rcfile=" + config
					default:
						return config
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				if got, want := req.shellcheckSettings.Config, actionlint.ShellcheckRCFile(inputPath(workspace, config)); got != want {
					t.Errorf("config = %q, want %q", got, want)
				}
			})
		}
	}
	for _, config := range []string{"missing", "."} {
		req := &lintRequest{shellcheck: "shellcheck", workingDir: workspace}
		err := req.configureShellcheck(func(key string) string {
			if key == "INPUT_SHELLCHECK-RC" {
				return config
			}
			return ""
		})
		if err == nil {
			t.Errorf("accepted config path %q", config)
		}
	}
}

func TestActionShellcheckExternalConfigSymlink(t *testing.T) {
	workspace := workspaceWith(t, nil)
	outside := workspaceWith(t, map[string]string{"rc": "disable=SC2086"})
	linkWorkspaceFile(t, filepath.Join(outside, "rc"), filepath.Join(workspace, "rc"))
	if _, err := shellcheckConfigPath(workspace, "rc"); err != nil {
		t.Fatalf("read linked config: %v", err)
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

func TestActionShellcheckSourcedDiagnostics(t *testing.T) {
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skipf("ShellCheck required: %s", err)
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	workspace := workspaceWith(t, map[string]string{
		".git": "", ".github/workflows/.gitkeep": "",
		"project/workflow.yml":     "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - working-directory: project/app\n        run: . ./lib/check.sh\n",
		"project/app/lib/check.sh": "# library\necho $VALUE\n",
	})
	for _, tc := range []struct{ name, args, inherited string }{
		{"long flag", `["--check-sourced"]`, ""},
		{"short flag", `["-a"]`, ""},
		{"inherited flag", `[]`, "--check-sourced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resultPath := filepath.Join(t.TempDir(), "result.json")
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_WORKING-DIRECTORY": "project", "INPUT_FILES": "workflow.yml",
				"ACTIONLINT_SHELLCHECK_COMMAND": command, "INPUT_PYFLAKES": "false", "INPUT_FORMAT": "github",
				"INPUT_SHELLCHECK-ARGS": tc.args, "SHELLCHECK_OPTS": tc.inherited, "ACTIONLINT_ACTION_RESULT": resultPath,
			}
			var out strings.Builder
			if code := Main(func(key string) string { return env[key] }, &out); code != 1 {
				t.Fatalf("want findings, got exit %d: %s", code, out.String())
			}
			if !strings.Contains(out.String(), "::error file=project/app/lib/check.sh,line=2,col=6,") || !strings.Contains(out.String(), "echo $VALUE") {
				t.Fatalf("wrong sourced annotation: %s", out.String())
			}
			var result persistedResult
			if err := json.Unmarshal([]byte(read(t, resultPath)), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want one persisted finding: %+v", result.Diagnostics)
			}
			finding := result.Diagnostics[0]
			if finding.Path != filepath.Join("app", "lib", "check.sh") || finding.Start.Line != 2 || finding.Snippet != "echo $VALUE" {
				t.Fatalf("wrong persisted sourced finding: %+v", finding)
			}
		})
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
		{name: "explicit file from working directory", config: "config with spaces"},
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
				"project/config with spaces": "disable=SC2086\n",
			})
			t.Setenv("GITHUB_WORKSPACE", workspace)
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_FILES": "workflow.yml",
				"INPUT_WORKING-DIRECTORY": "project", "INPUT_PYFLAKES": "false",
				"ACTIONLINT_SHELLCHECK_COMMAND": shellcheck,
				"INPUT_SHELLCHECK-RC":           tc.config, "INPUT_SHELLCHECK-ARGS": tc.args,
				"SHELLCHECK_OPTS": tc.inherited,
				"INPUT_CONFIG":    "tools: {" + tc.tools + "}",
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
