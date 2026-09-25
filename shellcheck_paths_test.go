package actionlint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
			writeShellcheckFixture(t, root, "scripts/build/config.sh", "VALUE=42\n")
			if runtime.GOOS != "windows" {
				writeShellcheckFixture(t, root, "a:debug/config.sh", "VALUE=42\n")
				writeShellcheckFixture(t, root, "C:/work/config.sh", "VALUE=42\n")
				value := "VALUE='two words'\n"
				if tc.literal {
					value = "VALUE=42\n"
				}
				writeShellcheckFixture(t, root, `scripts\build/config.sh`, value)
			}
			workflow := "on: push\n"
			if tc.scope == "workflow" {
				workflow += "defaults:\n  run:\n    working-directory: " + tc.directory + "\n"
			}
			workflow += "jobs:\n  test:\n    runs-on: " + tc.runner + "\n"
			if tc.scope == "job" {
				workflow += "    defaults:\n      run:\n        working-directory: " + tc.directory + "\n"
			}
			workflow += "    steps:\n      - shell: bash\n        run: |\n          . ./config.sh\n          echo $VALUE\n"
			if tc.scope == "step" {
				workflow += "        working-directory: " + tc.directory + "\n"
			}
			path := writeShellcheckFixture(t, root, ".github/workflows/test.yml", workflow)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
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
					t.Fatalf("unknown runner path must disable source following: %+v", findings)
				}
			} else if len(findings) != 0 {
				t.Fatalf("source not resolved with runner path semantics: %+v", findings)
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

func TestShellcheckWorkingDirectorySymlinks(t *testing.T) {
	root, outside, analyzer := t.TempDir(), t.TempDir(), t.TempDir()
	inside := filepath.Join(root, "scripts")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{
		{"internal", inside},
		{"external", outside},
		{"chain", filepath.Join(root, "external")},
		{"broken", filepath.Join(root, "missing")},
	} {
		if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
	}
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	for _, workspace := range []string{root, alias} {
		for _, directory := range []string{"internal", "external", "chain", "broken"} {
			t.Run(filepath.Base(workspace)+"/"+directory, func(t *testing.T) {
				rule := newRuleShellcheck(&externalCommand{})
				rule.paths.workspace, rule.paths.analysis = workspace, analyzer
				got := rule.stepDirectory(&ExecRun{WorkingDirectory: &String{Value: directory}})
				if directory != "broken" {
					target := outside
					if directory == "internal" {
						target = inside
					}
					canonical, err := filepath.EvalSymlinks(target)
					if err != nil {
						t.Fatal(err)
					}
					if got.kind != directoryKnown || got.path != canonical {
						t.Fatalf("available linked directory should resolve: %+v", got)
					}
				} else if got.kind != directoryUnknown || got.path != analyzer {
					t.Fatalf("unavailable local directory should use analysis fallback: %+v", got)
				}
			})
		}
	}
}

func TestShellcheckSymlinkDirectoryKeepsScriptAnalysis(t *testing.T) {
	command := shellcheckForTest(t)
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeShellcheckFixture(t, root, "scripts/value.sh", "VALUE=42\n")
	writeShellcheckFixture(t, outside, "value.sh", "VALUE=42\n")
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
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - working-directory: "+tc.name+"\n        run: |\n          . ./value.sh\n          echo $VALUE\n")
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
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
	writeShellcheckFixture(t, shared, "value.sh", "VALUE=42\n")
	runner := "ubuntu-latest"
	if runtime.GOOS == "windows" {
		runner = "windows-latest"
	}
	for _, directory := range []string{"../shared", shared} {
		t.Run(directory, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: "+runner+"\n    steps:\n      - shell: bash\n        working-directory: '"+directory+"'\n        run: |\n          . ./value.sh\n          echo $VALUE\n")
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("available sibling source not analyzed: %+v", result.Diagnostics)
			}
		})
	}
}

func TestShellcheckAbsoluteDirectoryPlatform(t *testing.T) {
	directory := t.TempDir()
	for _, platform := range []platformKind{platformKindAny, platformKindWindows, platformKindMacOrLinux} {
		path, known := shellcheckDirectoryPath(directory, platform)
		wantKnown := platform == platformKindAny ||
			(platform == platformKindWindows) == (runtime.GOOS == "windows")
		if known != wantKnown || known && path != filepath.Clean(directory) {
			t.Fatalf("platform %v: got %q, known=%v; want known=%v", platform, path, known, wantKnown)
		}
	}
}

func TestShellcheckWindowsRootedDirectory(t *testing.T) {
	for _, path := range []string{`\scripts\build`, `\\server\share`, `\\?\C:\scripts`} {
		if runtime.GOOS != "windows" {
			if got, known := shellcheckDirectoryPath(path, platformKindWindows); known {
				t.Errorf("Windows path %q mapped to Unix path %q", path, got)
			}
		}
	}
	if got, known := shellcheckDirectoryPath(`scripts\build`, platformKindWindows); !known || got != "scripts/build" {
		t.Errorf("relative Windows path lost: %q, known=%v", got, known)
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

func TestShellcheckApplicationSelectionPreservesInlineConfig(t *testing.T) {
	command := shellcheckForTest(t)
	for _, selection := range []string{"inherit", "file", "discover", "disabled"} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			rc := writeShellcheckFixture(t, root, ".shellcheckrc", "disable=SC2016\n")
			config := writeShellcheckFixture(t, root, "actionlint.yaml", "tools: {shellcheck: {config: {disable: [SC2086]}}}\n")
			workflow := writeShellcheckFixture(t, root, "workflow.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $VALUE\n")
			settings := &ShellcheckSettings{}
			switch selection {
			case "file":
				settings.Config = ShellcheckRCFile(rc)
			case "discover":
				settings.Config = ShellcheckRCDiscover
			case "disabled":
				settings.Config = ShellcheckRCDisabled
			}
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: config, WorkingDir: root, Shellcheck: command, ShellcheckSettings: settings})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
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
		t.Run(fmt.Sprint(explicitContext), func(t *testing.T) {
			root := t.TempDir()
			config := writeShellcheckFixture(t, root, "settings/actionlint.yaml", "tools: {shellcheck: {config: missing.rc}}\n")
			workingDir := filepath.Join(root, "project")
			base := workingDir
			selection := ".shellcheckrc"
			if explicitContext {
				base = filepath.Dir(config)
				selection = "${{ configdir }}/.shellcheckrc"
			}
			rc := writeShellcheckFixture(t, base, ".shellcheckrc", "disable=SC2086\n")
			workflow := writeShellcheckFixture(t, workingDir, "workflow.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $VALUE\n")
			overlay, err := ParseConfigOverlay("config", []byte("tools: {shellcheck: {config: '"+selection+"'}}"))
			if err != nil {
				t.Fatal(err)
			}
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: config, WorkingDir: workingDir, Shellcheck: command, ConfigOverlays: []ConfigOverlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, rc) {
				t.Fatalf("overlay rc origin lost: %+v", result)
			}
		})
	}
}
