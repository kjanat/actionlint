package application_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	toolconfig "actionlint.kjanat.dev/internal/config/tools"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestShellcheckExternalSourcesPrecedence(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, rc, environment string
		args                  []string
		unknown, follows      bool
	}{
		{name: "default", rc: "shell=bash\n", follows: true},
		{name: "rc disables", rc: "external-sources=false\n"},
		{name: "rc enables", rc: "external-sources=true\n", follows: true},
		{name: "quoted rc setting", rc: "shell=bash external-sources='false' # disabled\n"},
		{name: "native first setting wins", rc: "external-sources=false\nexternal-sources=true\n"},
		{name: "comment is not setting", rc: "# external-sources=false\nshell=bash\n", follows: true},
		{name: "quoted path is not setting", rc: "source-path='external-sources=false'\n", follows: true},
		{name: "rc disables despite explicit argument", rc: "external-sources=false\n", args: []string{"-x"}},
		{name: "rc disables despite environment", rc: "external-sources=false\n", environment: "--external-sources"},
		{name: "unknown directory default", rc: "external-sources=true\n", unknown: true},
		{name: "unknown directory explicit argument", rc: "external-sources=true\n", unknown: true, args: []string{"-x"}},
		{name: "unknown directory environment", rc: "external-sources=true\n", unknown: true, environment: "-x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHELLCHECK_OPTS", tc.environment)
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, "config.sh", "VALUE=42\n")
			writeFixture(t, root, ".github/.shellcheckrc", tc.rc)
			writeFixture(t, root, ".github/actionlint.yaml", `tools:
  shellcheck:
    config: .shellcheckrc
`)
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          . ./config.sh
          echo $VALUE
`
			if tc.unknown {
				workflow += "        working-directory: ${{ vars.DIRECTORY }}\n"
			}
			path := writeFixture(t, root, ".github/workflows/test.yml", workflow)
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: tc.args}}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.follows {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("source should establish VALUE as a number: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "SC2086") {
				t.Fatalf("source should not change the script analysis: %+v", result.Diagnostics)
			}
		})
	}
}

func TestShellcheckConfigPaths(t *testing.T) {
	command := shellcheckForTest(t)
	t.Setenv("GITHUB_WORKSPACE", "")
	for _, tc := range []struct {
		name, selection, rcname string
		explicit, overlay       bool
	}{
		{"relative", "./.shellcheckrc", ".shellcheckrc", false, false},
		{"configdir", "${{ configdir }}/.shellcheckrc", ".shellcheckrc", false, false},
		{"gitdir", "${{ gitdir }}/.github/.shellcheckrc", ".shellcheckrc", false, false},
		{"workspace", "${{ github.workspace }}/.github/.shellcheckrc", ".shellcheckrc", false, false},
		{"directory", "${{gitdir}}/.github/", ".shellcheckrc", false, false},
		{"directory without dotfile", "./", "shellcheckrc", false, false},
		{"explicit config outside repo", "${{ configdir }}/.shellcheckrc", ".shellcheckrc", true, false},
		{"overlay keeps configdir", "./.shellcheckrc", ".shellcheckrc", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			configdir := filepath.Join(root, ".github")
			if tc.explicit {
				configdir = t.TempDir()
			}
			rc := writeFixture(t, configdir, tc.rcname, "disable=SC2086\n")
			// A root-level decoy must not replace the config file's own directory.
			writeFixture(t, root, tc.rcname, "enable=all\n")
			config := writeFixture(t, configdir, "actionlint.yaml", `tools:
  shellcheck:
    config: '`+tc.selection+"'\n")
			workflow := writeFixture(t, root, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
`)
			options := application.Options{WorkingDir: root}
			optionsTools := adapters.Options{ShellCheck: command}
			if tc.explicit {
				options.ConfigFile = config
			}
			if tc.overlay {
				overlay, err := repoconfig.ParseOverlay("tools", []byte("shellcheck: true"))
				if err != nil {
					t.Fatal(err)
				}
				options.ConfigOverlays = []repoconfig.Overlay{overlay}
			}
			session := options
			session.Tools = optionsTools.Registrations()
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("rc file not applied: %+v", result.Diagnostics)
			}
			if !slices.Contains(result.Inputs, rc) {
				t.Fatalf("selected rc absent from inputs: %v", result.Inputs)
			}
		})
	}
}

func TestShellcheckSourceWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, workflowDefaults, jobDefaults, stepDir, sourceDir string
		unknown                                                 bool
	}{
		{"workspace", "", "", "", "scripts", false},
		{"workflow default", "workflow", "", "", "workflow/scripts", false},
		{"job default", "workflow", "job", "", "job/scripts", false},
		{"step override", "workflow", "job", "step", "step/scripts", false},
		{"empty step overrides defaults", "workflow", "job", "''", "scripts", false},
		{"literal expression", "", "", "${{ 'step' }}", "step/scripts", false},
		{"dynamic blocks fallback", "workflow", "job", "${{ vars.DIRECTORY }}", "job/scripts", true},
		{"missing runtime directory", "", "", "created-at-runtime", "scripts", true},
		{"colon in directory", "", "", "release:debug", "release:debug/scripts", false},
		{"colon in nested directory", "", "", "nested/release:debug", "nested/release:debug/scripts", false},
		{"Unix drive-like relative name", "", "", "C:work", "C:work/scripts", false},
		{"Unix drive-like nested name", "", "", "C:/work", "C:/work/scripts", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(tc.stepDir, ":") {
				t.Skip("fixture requires Unix filenames containing colons")
			}
			root := t.TempDir()
			for _, dir := range []string{".git", "step", "job", "workflow"} {
				if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeFixture(t, root, ".github/actionlint.yaml", `tools:
  shellcheck:
    config:
      source-path: [scripts]
      external-sources: true
`)
			writeFixture(t, root, tc.sourceDir+"/config.sh", "VALUE=42\n")
			// The analyzer runs elsewhere, where a different source value would warn.
			analyzer := t.TempDir()
			decoy := "VALUE='two words'\n"
			if tc.unknown {
				decoy = "VALUE=42\n"
			}
			writeFixture(t, analyzer, "scripts/config.sh", decoy)
			t.Chdir(analyzer)
			workflow := "on: push\n"
			if tc.workflowDefaults != "" {
				workflow += `defaults:
  run:
    working-directory: ` + tc.workflowDefaults + "\n"
			}
			workflow += `jobs:
  test:
    runs-on: ubuntu-latest
`
			if tc.jobDefaults != "" {
				workflow += `    defaults:
      run:
        working-directory: ` + tc.jobDefaults + "\n"
			}
			workflow += `    steps:
      - run: |
          . config.sh
          echo $VALUE
`
			if tc.stepDir != "" {
				workflow += "        working-directory: " + tc.stepDir + "\n"
			}
			path := writeFixture(t, root, ".github/workflows/test.yml", workflow)
			session := application.Options{WorkingDir: analyzer, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var findings []diagmodel.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "shellcheck" {
					findings = append(findings, diagnostic)
				}
			}
			if tc.unknown {
				if len(findings) != 1 || !strings.Contains(findings[0].Message, "SC2086") {
					t.Fatalf("unknown directory must retain independent script analysis: %+v", findings)
				}
			} else if len(findings) != 0 {
				t.Fatalf("source not resolved from run directory: %+v", findings)
			}
		})
	}
}

func TestShellcheckRunnerWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, runner, directory, scope string
		literal, unknown               bool
	}{
		{"Windows step", "windows-latest", `scripts\build`, "step", false, false},
		{"Windows job default", "windows-latest", `scripts\build`, "job", false, false},
		{"Windows workflow default", "windows-latest", `scripts\build`, "workflow", false, false},
		{"Unix literal backslash", "ubuntu-latest", `scripts\build`, "step", true, runtime.GOOS == "windows"},
		{"Unix slash", "ubuntu-latest", "scripts/build", "step", false, false},
		{"Linux single-letter colon", "ubuntu-latest", "a:debug", "step", false, runtime.GOOS == "windows"},
		{"macOS single-letter colon", "macos-latest", "a:debug", "step", false, runtime.GOOS == "windows"},
		{"Windows drive relative", "windows-latest", "a:debug", "step", false, true},
		{"Windows drive absolute", "windows-latest", "C:/work", "step", false, true},
		{"unknown colon semantics", "self-hosted", "a:debug", "step", false, true},
		{"unknown runner", "self-hosted", `scripts\build`, "step", true, true},
		{"conflicting platforms", "[self-hosted, windows, linux]", `scripts\build`, "step", true, true},
		{"Windows rooted", "windows-latest", `\scripts\build`, "step", false, true},
		{"Windows UNC", "windows-latest", `\\server\share`, "step", false, true},
		{"Windows escape", "windows-latest", `..\..\scripts\build`, "step", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, "scripts/build/config.sh", "VALUE=42\n")
			if runtime.GOOS != "windows" {
				writeFixture(t, root, "a:debug/config.sh", "VALUE=42\n")
				writeFixture(t, root, "C:/work/config.sh", "VALUE=42\n")
				value := "VALUE='two words'\n"
				if tc.literal {
					value = "VALUE=42\n"
				}
				writeFixture(t, root, `scripts\build/config.sh`, value)
			}
			workflow := "on: push\n"
			if tc.scope == "workflow" {
				workflow += `defaults:
  run:
    working-directory: ` + tc.directory + "\n"
			}
			workflow += `jobs:
  test:
    runs-on: ` + tc.runner + "\n"
			if tc.scope == "job" {
				workflow += `    defaults:
      run:
        working-directory: ` + tc.directory + "\n"
			}
			workflow += `    steps:
      - shell: bash
        run: |
          . ./config.sh
          echo $VALUE
`
			if tc.scope == "step" {
				workflow += "        working-directory: " + tc.directory + "\n"
			}
			path := writeFixture(t, root, ".github/workflows/test.yml", workflow)
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var findings []diagmodel.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "shellcheck" {
					findings = append(findings, diagnostic)
				}
			}
			if tc.unknown {
				if len(findings) != 1 || !strings.Contains(findings[0].Message, "SC2086") {
					t.Fatalf("unknown runner path must disable source following: %+v", findings)
				}
			} else if len(findings) != 0 {
				t.Fatalf("source not resolved with runner path semantics: %+v", findings)
			}
		})
	}
}

func TestShellcheckSymlinkDirectoryKeepsScriptAnalysis(t *testing.T) {
	command := shellcheckForTest(t)
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "scripts/value.sh", "VALUE=42\n")
	writeFixture(t, outside, "value.sh", "VALUE=42\n")
	for _, tc := range []struct {
		name, target string
		wantFinding  bool
	}{
		{"internal", filepath.Join(root, "scripts"), false},
		{"external", outside, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Symlink(tc.target, filepath.Join(root, tc.name)); err != nil {
				t.Skipf("symlinks are unavailable: %v", err)
			}
			workflow := writeFixture(t, root, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - working-directory: `+tc.name+`
        run: |
          . ./value.sh
          echo $VALUE
`)
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "shellcheck" && strings.Contains(d.Message, "SC2086")
			})
			if found != tc.wantFinding {
				t.Fatalf("available source must be analyzed; SC2086 = %v, diagnostics: %+v", found, result.Diagnostics)
			}
		})
	}
}

func TestShellcheckSiblingWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	parent := t.TempDir()
	root, shared := filepath.Join(parent, "project"), filepath.Join(parent, "shared")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, shared, "value.sh", "VALUE=42\n")
	runner := "ubuntu-latest"
	if runtime.GOOS == "windows" {
		runner = "windows-latest"
	}
	for _, directory := range []string{"../shared", shared} {
		t.Run(directory, func(t *testing.T) {
			workflow := writeFixture(t, root, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - shell: bash
        working-directory: '`+directory+`'
        run: |
          . ./value.sh
          echo $VALUE
`)
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("available sibling source not analyzed: %+v", result.Diagnostics)
			}
		})
	}
}

func TestShellcheckSelectedConfigFailure(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, config, content string
		disabled              bool
	}{
		{"missing file", "missing.rc", "", false},
		{"invalid rc", "broken.rc", "disable=not-a-code\n", false},
		{"explicit disabled overrides missing file", "missing.rc", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.content != "" {
				writeFixture(t, root, tc.config, tc.content)
			}
			config := writeFixture(t, root, "actionlint.yaml", "tools: {shellcheck: {config: "+tc.config+"}}\n")
			workflow := writeFixture(t, root, "workflow.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
`)
			options := application.Options{ConfigFile: config, WorkingDir: root}
			optionsTools := adapters.Options{ShellCheck: command}
			if tc.disabled {
				optionsTools.ShellcheckSettings = &toolconfig.ShellcheckSettings{Config: toolconfig.ShellcheckRCDisabled}
			}
			session := options
			session.Tools = optionsTools.Registrations()
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if !tc.disabled {
				if err == nil {
					t.Fatalf("invalid selected config silently accepted: %+v", result.Diagnostics)
				}
			} else if err != nil || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "SC2086") {
				t.Fatalf("disabling rc loading must retain linting: %+v, %v", result, err)
			}
		})
	}
}

func TestShellcheckApplicationSelectionPreservesInlineConfig(t *testing.T) {
	command := shellcheckForTest(t)
	for _, selection := range []string{"inherit", "file", "discover", "disabled"} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			rc := writeFixture(t, root, ".shellcheckrc", "disable=SC2016\n")
			config := writeFixture(t, root, "actionlint.yaml", "tools: {shellcheck: {config: {disable: [SC2086]}}}\n")
			workflow := writeFixture(t, root, "workflow.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
`)
			settings := &toolconfig.ShellcheckSettings{}
			switch selection {
			case "file":
				settings.Config = toolconfig.ShellcheckRCFile(rc)
			case "discover":
				settings.Config = toolconfig.ShellcheckRCDiscover
			case "disabled":
				settings.Config = toolconfig.ShellcheckRCDisabled
			}
			session := application.Options{ConfigFile: config, WorkingDir: root, Tools: (adapters.Options{ShellCheck: command, ShellcheckSettings: settings}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("rc selection must retain inline directives: %+v", result.Diagnostics)
			}
		})
	}
}

func TestShellcheckOverlayPathOrigin(t *testing.T) {
	command := shellcheckForTest(t)
	for _, explicitContext := range []bool{false, true} {
		t.Run(strconv.FormatBool(explicitContext), func(t *testing.T) {
			root := t.TempDir()
			config := writeFixture(t, root, "settings/actionlint.yaml", "tools: {shellcheck: {config: missing.rc}}\n")
			workingDir := filepath.Join(root, "project")
			base := workingDir
			selection := ".shellcheckrc"
			if explicitContext {
				base = filepath.Dir(config)
				selection = "${{ configdir }}/.shellcheckrc"
			}
			rc := writeFixture(t, base, ".shellcheckrc", "disable=SC2086\n")
			workflow := writeFixture(t, workingDir, "workflow.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
`)
			overlay, err := repoconfig.ParseOverlay("config", []byte("tools: {shellcheck: {config: '"+selection+"'}}"))
			if err != nil {
				t.Fatal(err)
			}
			session := application.Options{ConfigFile: config, WorkingDir: workingDir, ConfigOverlays: []repoconfig.Overlay{overlay}, Tools: (adapters.Options{ShellCheck: command}).Registrations()}

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, rc) {
				t.Fatalf("overlay rc origin lost: %+v", result)
			}
		})
	}
}
