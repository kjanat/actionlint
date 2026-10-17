package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestExplicitActionSnapshotAliasedRoot(t *testing.T) {
	for _, disk := range []bool{false, true} {
		for _, manifest := range []string{
			standaloneComposite,
			"name: inner\ndescription: inner\nruns: {using: node24}\n",
			"name: [\n",
			strings.Replace(standaloneComposite, "name: example\n", "", 1),
			strings.Replace(standaloneComposite, "description: example\n", "", 1),
			standaloneComposite + "branding: {icon: invalid-icon}\n",
		} {
			t.Run(fmt.Sprintf("disk=%t/manifest=%q", disk, manifest), func(t *testing.T) {
				root := t.TempDir()
				if disk {
					writeShellcheckFixture(t, root, "inner/action.yml", "stale: true\n")
				}
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, alias); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				project := workspace.Identity(root)
				workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
				result, err := runTestAnalysis(t.Context(), Request{WorkingDir: alias, Sources: []SourceUnit{
					{Path: "ci.yml", Content: []byte(workflow), Project: project},
					{Path: "inner/action.yml", Content: []byte(manifest), Project: project},
				}})
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if manifest == standaloneComposite {
					want = 0
				}
				if len(result.Diagnostics) != want {
					t.Fatalf("want %d snapshot findings, got %+v", want, result.Diagnostics)
				}
				if len(result.Documents) != 2 {
					t.Errorf("want one workflow and one action outline, got %+v", result.Documents)
				}
				for _, diagnostic := range result.Diagnostics {
					if filepath.Base(diagnostic.Path) != "action.yml" {
						t.Errorf("finding must belong to explicit declaration: %+v", diagnostic)
					}
				}
			})
		}
	}
}

func TestExplicitActionSnapshotAndDeduplication(t *testing.T) {
	for _, disk := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
				t.Run(fmt.Sprintf("disk=%t/reverse=%t/%s", disk, reverse, runner), func(t *testing.T) {
					root := t.TempDir()
					if disk {
						writeShellcheckFixture(t, root, "inner/action.yml", standaloneComposite)
					}
					manifest := "name: inner\ndescription: inner\nruns:\n  using: node24\n"
					workflow := "on: push\njobs:\n  test:\n    runs-on: " + runner + "\n    steps:\n      - uses: ./inner\n        with: {unknown: value}\n"
					project := workspace.Identity(root)
					sources := []SourceUnit{
						{Path: "ci.yml", Content: []byte(workflow), Project: project},
						{Path: "inner/action.yml", Content: []byte(manifest), Project: project},
					}
					if reverse {
						sources[0], sources[1] = sources[1], sources[0]
					}
					result, err := runTestAnalysis(t.Context(), Request{WorkingDir: root, Sources: sources})
					if err != nil {
						t.Fatal(err)
					}
					missingMain, unknownInput := 0, 0
					for _, d := range result.Diagnostics {
						switch {
						case strings.Contains(d.Message, "main") && strings.Contains(d.Message, "required"):
							missingMain++
							if d.Path != filepath.Join("inner", "action.yml") {
								t.Errorf("metadata finding should use declaration, got %+v", d)
							}
						case strings.Contains(d.Message, "unknown"):
							unknownInput++
						default:
							t.Errorf("unexpected diagnostic %+v", d)
						}
					}
					if missingMain != 1 || unknownInput != 1 {
						t.Fatalf("want one declaration error and one caller input error; main=%d input=%d diagnostics=%+v", missingMain, unknownInput, result.Diagnostics)
					}
				})
			}
		}
	}
}

func TestRelativeProjectExplicitActionSnapshot(t *testing.T) {
	for _, projectRoot := range []string{".", "repo"} {
		for _, disk := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/disk=%t", projectRoot, disk), func(t *testing.T) {
				cwd := t.TempDir()
				t.Chdir(cwd)
				root := filepath.Join(cwd, projectRoot)
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatal(err)
				}
				if disk {
					writeShellcheckFixture(t, root, "inner/action.yml", "name: stale\nruns: {using: node24}\n")
				}
				project, err := workspace.NewProject(projectRoot)
				if err != nil {
					t.Fatal(err)
				}
				workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
				result, err := runTestAnalysis(t.Context(), Request{WorkingDir: cwd, Sources: []SourceUnit{
					{Path: filepath.Join(projectRoot, "ci.yml"), Content: []byte(workflow), Project: project},
					{Path: filepath.Join(projectRoot, "inner/action.yml"), Content: []byte(standaloneComposite), Project: project},
				}})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 0 {
					t.Fatalf("explicit valid snapshot should override missing/stale disk metadata: %+v", result.Diagnostics)
				}
				if project.RootDir() != projectRoot {
					t.Fatalf("analysis mutated caller project: root=%q", project.RootDir())
				}
			})
		}
	}
}

func TestSelectedActionDeclarationPreference(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, tc := range []struct {
			name, manifest, config, rule, path, severity string
		}{
			{"severity", "name: inner\ndescription: inner\nruns: {using: node24}\n", "overrides: [{includes: [inner/action.yml], lint: {rules: {correctness: {action: warn}}}}]", "action", "inner/action.yml", "warning"},
			{"malformed", "name: [\n", "", "syntax-check", "inner/action.yml", ""},
			{"malformed-declaration-disabled", "name: [\n", "overrides: [{includes: [inner/action.yml], lint: {rules: {correctness: {syntax-check: off}}}}]", "action", "ci.yml", ""},
		} {
			t.Run(fmt.Sprintf("%s/reverse=%t", tc.name, reverse), func(t *testing.T) {
				root := t.TempDir()
				cfg, err := repoconfig.Parse([]byte(tc.config))
				if err != nil {
					t.Fatal(err)
				}
				project := workspace.Identity(root)
				workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
				sources := []SourceUnit{
					{Path: "ci.yml", Content: []byte(workflow), Project: project, Config: cfg},
					{Path: "inner/action.yml", Content: []byte(tc.manifest), Project: project, Config: cfg},
				}
				if reverse {
					sources[0], sources[1] = sources[1], sources[0]
				}
				result, err := runTestAnalysis(t.Context(), Request{WorkingDir: root, Sources: sources})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 1 {
					t.Fatalf("want exactly one visible finding, got %+v", result.Diagnostics)
				}
				d := result.Diagnostics[0]
				if d.Rule != tc.rule || filepath.ToSlash(d.Path) != tc.path || d.Severity != tc.severity {
					t.Fatalf("want %s at %s with severity %q, got %+v", tc.rule, tc.path, tc.severity, d)
				}
			})
		}
	}
}
