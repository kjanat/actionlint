package cli_test

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/cli"
	"actionlint.kjanat.dev/internal/testutil"
)

func TestConfigInitAliases(t *testing.T) {
	for _, args := range [][]string{
		{"config", "init"}, {"init"}, {"init-config"},
		{"--init"}, {"--init-config"}, {"-init"},
		{"config", "--init"}, {"config", "--init-config"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			testutil.NewDefaultPluginRegistry(t)
			commandTestRepo(t)
			t.Setenv("NO_HYPERLINKS", "1")
			got := testRunCommand("", args...)
			want := "Created " + filepath.Join(".github", "actionlint.yaml") + "\n"
			if got.Status != 0 || got.Stderr != "" || got.Stdout != want {
				t.Fatalf("%+v", got)
			}
			before, err := os.ReadFile(".github/actionlint.yaml")
			if err != nil || !bytes.Contains(before, []byte("# tools:")) {
				t.Fatalf("reference not generated: %s, %v", before, err)
			}
			got = testRunCommand("", args...)
			after, err := os.ReadFile(".github/actionlint.yaml")
			if got.Status != 3 || !strings.Contains(got.Stderr, "already exists") || err != nil || !bytes.Equal(before, after) {
				t.Fatalf("existing config was not protected: %+v, %v", got, err)
			}
		})
	}
}

func TestConfigInitRelativeHyperlink(t *testing.T) {
	for _, mode := range []string{"always", "never"} {
		t.Run(mode, func(t *testing.T) {
			testutil.NewDefaultPluginRegistry(t)
			commandTestRepo(t)
			if err := os.Mkdir("nested", 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir("nested")
			got := testRunCommand("", "config", "init", "--hyperlinks="+mode)
			path := filepath.Join("..", ".github", "actionlint.yaml")
			display := path
			if mode == "always" {
				absolute, err := filepath.Abs(path)
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.ToSlash(absolute)
				if runtime.GOOS == "windows" {
					target = "/" + target
				}
				link := (&url.URL{Scheme: "file", Path: target}).String()
				display = "\x1b]8;;" + link + "\x1b\\" + path + "\x1b]8;;\x1b\\"
			}
			want := "Created " + display + "\n"
			if got.Status != 0 || got.Stdout != want || got.Stderr != "" {
				t.Fatalf("%+v; want %q", got, want)
			}
		})
	}
}

func TestConfigInitThroughRepositoryAlias(t *testing.T) {
	testutil.NewDefaultPluginRegistry(t)
	root := t.TempDir()
	gitRepository(t, root)
	alias := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Chdir(alias)
	t.Setenv("PWD", alias)
	got := testRunCommand("", "config", "init", "--hyperlinks=never")
	want := "Created " + filepath.Join(".github", "actionlint.yaml") + "\n"
	if got.Status != 0 || got.Stdout != want || got.Stderr != "" {
		t.Fatalf("repository alias changed display path: %+v; want %q", got, want)
	}
}

func TestCompletionGuidance(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	for _, name := range []string{"completion", "completions"} {
		got := testRunCommand("", name)
		if got.Status != 0 || got.Stdout != "" || !strings.Contains(got.Stderr, "Supported shells:") || !strings.Contains(got.Stderr, "/path/on/fpath/_actionlint") || strings.Contains(got.Stderr, "Compatibility:") {
			t.Fatalf("%+v", got)
		}
		got = testRunCommand("", name, "zsh", "fish")
		if got.Status != 2 || !strings.Contains(got.Stderr, "Usage: actionlint completion <shell>") {
			t.Fatalf("%+v", got)
		}
	}
	if singular, plural := testRunCommand("", "completion", "zsh"), testRunCommand("", "completions", "zsh"); singular.Status != 0 || singular != plural {
		t.Fatal("completion alias changed script output")
	}
}

func TestCompletionExamplesFollowDetectedShell(t *testing.T) {
	t.Setenv("PATHEXT", "")
	for _, tc := range []struct {
		name              string
		shell             string
		powerShellPresent bool
		want              string
		unwanted          string
		detected          string
	}{
		{"bash", "/bin/bash", false, "source <(actionlint completion bash)", "completion zsh", "bash"},
		{"zsh", "/usr/bin/zsh", false, "source <(actionlint completion zsh)", "Invoke-Expression", "zsh"},
		{"fish", "/usr/bin/fish", false, "actionlint completion fish | source", "source <(", "fish"},
		{"powershell", "", true, "actionlint completion powershell | Out-String | Invoke-Expression", "source <(", "powershell"},
		{"windows wrapper", `C:\tools\pwsh.cmd`, false, "Invoke-Expression", "source <(", "powershell"},
		{"windows forward slashes", "C:/Program Files/PowerShell/7/pwsh.exe", false, "Invoke-Expression", "source <(", "powershell"},
		{"windows mixed separators", `C:\Program Files/PowerShell\7/pwsh.exe`, false, "Invoke-Expression", "source <(", "powershell"},
		{"login shell wins", "/bin/bash", true, "source <(actionlint completion bash)", "Invoke-Expression", "bash"},
		{"unsupported shell fallback", "/bin/ksh", true, "Invoke-Expression", "source <(", "powershell"},
		{"unknown", "/bin/ksh", false, "actionlint completion bash\n", "source <(", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHELL", tc.shell)
			// An empty, present PSModulePath is still the DreamCLI fallback signal.
			t.Setenv("PSModulePath", "")
			if !tc.powerShellPresent {
				if err := os.Unsetenv("PSModulePath"); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range [][]string{{"completion"}, {"completion", "--help"}, {"completions", "--help"}} {
				got := testRunCommand("", args...)
				if got.Status != 0 || !strings.Contains(got.Stderr, tc.want) || strings.Contains(got.Stderr, tc.unwanted) {
					t.Fatalf("%v: %+v", args, got)
				}
			}
			if tc.detected != "" {
				if auto, explicit := testRunCommand("", "completion", "auto"), testRunCommand("", "completion", tc.detected); auto.Status != 0 || auto != explicit {
					t.Fatal("auto detection disagrees with help examples")
				}
			}
		})
	}
}

func TestCompletionExamplesAndAutoUsePathExt(t *testing.T) {
	t.Setenv("PATHEXT", ".EXE;.CUSTOM")
	t.Setenv("SHELL", `C:\tools\pwsh.CUSTOM`)
	t.Setenv("PSModulePath", "")
	if err := os.Unsetenv("PSModulePath"); err != nil {
		t.Fatal(err)
	}
	help := testRunCommand("", "completion", "--help")
	if help.Status != 0 || !strings.Contains(help.Stderr, "Invoke-Expression") {
		t.Fatalf("help ignores PATHEXT: %+v", help)
	}
	if auto, explicit := testRunCommand("", "completion", "auto"), testRunCommand("", "completion", "powershell"); auto.Status != 0 || auto != explicit {
		t.Fatal("auto detection ignores PATHEXT")
	}
}

func TestNoninteractiveReportOverwriteRequiresConfirmation(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmtTestName(modern, "report"), func(t *testing.T) {
			commandTestRepo(t)
			if err := os.WriteFile("report.json", []byte("previous report"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--json", "--output-file", "report.json"}
			if modern {
				args = append([]string{"check"}, args...)
			}
			// The destination error must win over a missing input: no analysis yet.
			got := testRunCommand("", append(args, "missing.yml")...)
			data, err := os.ReadFile("report.json")
			if got.Status != 3 || !strings.Contains(got.Stderr, "--yes") || err != nil || string(data) != "previous report" {
				t.Fatalf("%+v: %s, %v", got, data, err)
			}
			got = testRunCommand("", append(args, "--yes")...)
			data, err = os.ReadFile("report.json")
			if got.Status != 1 || err != nil || string(data) == "previous report" {
				t.Fatalf("explicit replacement failed: %+v, %v", got, err)
			}
			before := bytes.Clone(data)
			got = testRunCommand("", append(args, "--yes", "missing.yml")...)
			data, err = os.ReadFile("report.json")
			if got.Status != 3 || err != nil || !bytes.Equal(before, data) {
				t.Fatalf("failed analysis changed report: %+v, %v", got, err)
			}
		})
	}
}

func TestReportOverwriteEnvironmentAndSymlinks(t *testing.T) {
	commandTestRepo(t)
	t.Setenv("ACTIONLINT_OUTPUT_FILE", "report.json")
	if err := os.WriteFile("report.json", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := testRunCommand("", "check", "missing.yml"); got.Status != 3 || !strings.Contains(got.Stderr, "already exists") {
		t.Fatal(got)
	}
	if got := testRunCommand("", "check", "--output-file=-"); got.Status != 1 || got.Stdout == "" {
		t.Fatal(got)
	}
	if err := os.Symlink("missing-target", "dangling"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := testRunCommand("", "check", "--output-file=dangling"); got.Status != 3 || !strings.Contains(got.Stderr, "already exists") {
		t.Fatal(got)
	}
	if _, err := os.Stat("missing-target"); !os.IsNotExist(err) {
		t.Fatal("dangling symlink target created", err)
	}
}

type reportCreatingInput struct {
	*strings.Reader
	destination string
}

func (r *reportCreatingInput) Read(p []byte) (int, error) {
	if r.destination != "" {
		if err := os.WriteFile(r.destination, []byte("created during analysis"), 0600); err != nil {
			return 0, err
		}
		r.destination = ""
	}
	return r.Reader.Read(p)
}

func TestReportCreatedDuringAnalysisIsPreserved(t *testing.T) {
	commandTestRepo(t)
	input := &reportCreatingInput{Reader: strings.NewReader(commandGoodWorkflow), destination: "report.json"}
	var out, stderr bytes.Buffer
	command := cli.Command{Stdin: input, Stdout: &out, Stderr: &stderr}
	status := command.Main([]string{"actionlint", "check", "--json", "--output-file=report.json", "--shellcheck=", "--ruff=", "-"})
	data, err := os.ReadFile("report.json")
	if status != 3 || !strings.Contains(stderr.String(), "already exists") || err != nil || string(data) != "created during analysis" {
		t.Fatalf("destination replaced: %d, %s, %s, %v", status, &stderr, data, err)
	}
	files, err := filepath.Glob(".actionlint-report-*")
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary reports remain: %v, %v", files, err)
	}
}
