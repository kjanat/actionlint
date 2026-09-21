package actionlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeShellcheckFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellcheckForTest(t *testing.T) string {
	t.Helper()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	return command
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
			rc := writeShellcheckFixture(t, configdir, tc.rcname, "disable=SC2086\n")
			// A root-level decoy must not replace the config file's own directory.
			writeShellcheckFixture(t, root, tc.rcname, "enable=all\n")
			config := writeShellcheckFixture(t, configdir, "actionlint.yaml", "tools:\n  shellcheck:\n    config: '"+tc.selection+"'\n")
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $VALUE\n")
			options := AnalysisOptions{WorkingDir: root, Shellcheck: command}
			if tc.explicit {
				options.ConfigFile = config
			}
			if tc.overlay {
				overlay, err := ParseConfigOverlay("tools", []byte("shellcheck: true"))
				if err != nil {
					t.Fatal(err)
				}
				options.ConfigOverlays = []ConfigOverlay{overlay}
			}
			session, err := NewAnalysisSession(options)
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
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
		{"dynamic blocks fallback", "workflow", "job", "${{ github.event.inputs.directory }}", "job/scripts", true},
		{"missing runtime directory", "", "", "created-at-runtime", "scripts", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{".git", "step", "job", "workflow"} {
				if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools:\n  shellcheck:\n    config:\n      source-path: [scripts]\n      external-sources: true\n")
			writeShellcheckFixture(t, root, tc.sourceDir+"/config.sh", "VALUE=42\n")
			// The analyzer runs elsewhere, where a different source value would warn.
			analyzer := t.TempDir()
			decoy := "VALUE='two words'\n"
			if tc.unknown {
				decoy = "VALUE=42\n"
			}
			writeShellcheckFixture(t, analyzer, "scripts/config.sh", decoy)
			t.Chdir(analyzer)
			workflow := "on: push\n"
			if tc.workflowDefaults != "" {
				workflow += "defaults:\n  run:\n    working-directory: " + tc.workflowDefaults + "\n"
			}
			workflow += "jobs:\n  test:\n    runs-on: ubuntu-latest\n"
			if tc.jobDefaults != "" {
				workflow += "    defaults:\n      run:\n        working-directory: " + tc.jobDefaults + "\n"
			}
			workflow += "    steps:\n      - run: |\n          . config.sh\n          echo $VALUE\n"
			if tc.stepDir != "" {
				workflow += "        working-directory: " + tc.stepDir + "\n"
			}
			path := writeShellcheckFixture(t, root, ".github/workflows/test.yml", workflow)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: analyzer, Shellcheck: command})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{path}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var findings []Diagnostic
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

func TestShellcheckConfigDirectorySelection(t *testing.T) {
	root := t.TempDir()
	if _, err := shellcheckRCFile(root); err == nil {
		t.Fatal("accepted empty config directory")
	}
	writeShellcheckFixture(t, root, "shellcheckrc", "enable=all\n")
	dotfile := writeShellcheckFixture(t, root, ".shellcheckrc", "disable=SC2086\n")
	got, err := shellcheckRCFile(root)
	if err != nil || got != dotfile {
		t.Fatalf("want dotfile first, got %q: %v", got, err)
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
				writeShellcheckFixture(t, root, tc.config, tc.content)
			}
			config := writeShellcheckFixture(t, root, "actionlint.yaml", "tools: {shellcheck: {config: "+tc.config+"}}\n")
			workflow := writeShellcheckFixture(t, root, "workflow.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $VALUE\n")
			options := AnalysisOptions{ConfigFile: config, WorkingDir: root, Shellcheck: command}
			if tc.disabled {
				options.ShellcheckSettings = &ShellcheckSettings{Config: ShellcheckRCDisabled}
			}
			session, err := NewAnalysisSession(options)
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
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
