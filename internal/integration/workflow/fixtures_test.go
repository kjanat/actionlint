package workflow_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"

	"golang.org/x/sys/execabs"
)

func TestLinterMissingLocalAction(t *testing.T) {
	root := t.TempDir()
	proj := workspace.Identity(root)
	for _, tc := range []struct {
		steps string
		want  int
	}{
		{"      - uses: ./missing\n      - uses: ./missing\n      - uses: $/missing\n", 3},
		{"      - uses: ./missing # actionlint:ignore missing-local-action -- downloaded at runtime\n", 0},
		{"      - uses: ./missing\n        id: missing\n", 1},
		{"      - uses: ./missing # actionlint:ignore missing-local-action -- downloaded at runtime\n        id: missing\n", 0},
	} {
		request := application.Options{}
		var requestSources []workspace.Source
		var lConfig *repoconfig.Config
		var err error

		lConfig = &repoconfig.Config{}
		source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + tc.steps
		requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: proj, Config: lConfig}}
		errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
		if err != nil || len(errs) != tc.want {
			t.Fatalf("got %v, %v; want %d diagnostics", errs, err, tc.want)
		}
		for i, diagnostic := range errs {
			if diagnostic.Rule != "missing-local-action" || diagnostic.Start.Line != 6+i || !strings.Contains(diagnostic.Message, "has no action.yml, action.yaml") {
				t.Fatalf("unexpected diagnostic: %+v", diagnostic)
			}
		}
	}
}

func TestMissingLocalActionRuleSelection(t *testing.T) {
	for _, tc := range []struct {
		rules string
		want  string
	}{
		{"action: off", "missing-local-action"},
		{"missing-local-action: off", "action"},
	} {
		t.Run(tc.rules, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("lint: {rules: {correctness: {" + tc.rules + "}}}"))
			if err != nil {
				t.Fatal(err)
			}
			request := application.Options{}
			var requestSources []workspace.Source
			lConfig := cfg
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./missing\n      - uses: invalid\n"
			requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: workspace.Identity(t.TempDir()), Config: lConfig}}
			findings, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil || len(findings) != 1 || findings[0].Rule != tc.want {
				t.Fatalf("got %v, %v; want only %s", findings, err, tc.want)
			}
		})
	}
}

func TestWorkflowFixturesOK(t *testing.T) {
	dir := filepath.Join("testdata", "ok")

	es, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	fs := make([]string, 0, len(es))
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			fs = append(fs, filepath.Join(dir, n))
		}
	}

	proj := workspace.Identity(dir)
	shellcheck, _ := execabs.LookPath("shellcheck")

	for _, f := range fs {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if strings.Contains(f, "shellcheck") && shellcheck == "" {
				t.Skip("skipping", f, "because \"shellcheck\" command does not exist in system")
			}

			request := application.Options{Tools: adapters.Options{ShellCheck: shellcheck}.Registrations()}
			requestSources := testutil.FixtureSources(t, []string{f}, proj, &repoconfig.Config{})
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) > 0 {
				t.Fatal(errs)
			}
		})
	}
}

func testFindAllWorkflowsInDir(subdir string) (string, []string, error) {
	dir := filepath.Join("testdata", subdir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}

	fs := make([]string, 0, len(entries))
	for _, info := range entries {
		if info.IsDir() {
			continue
		}
		n := info.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			fs = append(fs, filepath.Join(dir, n))
		}
	}

	return dir, fs, nil
}

func checkErrors(t *testing.T, outfile string, errs []diagmodel.Diagnostic) {
	t.Helper()
	t.Logf("Checking %d errors with %q", len(errs), outfile)
	for i, err := range errs {
		t.Logf("  Error[%d]: %s", i, formatDiagnostic(err))
	}

	expected := []string{}
	{
		f, err := os.Open(outfile)
		if err != nil {
			panic(err)
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			expected = append(expected, s.Text())
		}
		if err := s.Err(); err != nil {
			panic(err)
		}
	}

	slices.SortFunc(errs, func(a, b diagmodel.Diagnostic) int {
		return findingmodel.Compare(findingmodel.FromDiagnostic(a), findingmodel.FromDiagnostic(b))
	})

	if len(errs) != len(expected) {
		ms := make([]string, 0, len(errs))
		for _, err := range errs {
			ms = append(ms, formatDiagnostic(err))
		}
		t.Fatalf("%d errors are expected but actually got %d errors:\n%v", len(expected), len(errs), strings.Join(ms, "\n"))
	}

	for i := range errs {
		errs[i].Path = filepath.ToSlash(errs[i].Path) // For Windows
		want, have := expected[i], formatDiagnostic(errs[i])
		if strings.HasPrefix(want, "/") && strings.HasSuffix(want, "/") {
			want := regexp.MustCompile(want[1 : len(want)-1])
			if !want.MatchString(have) {
				t.Errorf(`error message mismatch at %dth error does not match to regular expression
  want: /%s/
  have: %q`, i+1, want, have)
			}
		} else if want != have {
			t.Errorf(`error message mismatch at %dth error does not match exactly
  want: %q
  have: %q`, i+1, want, have)
		}
	}
}

func TestWorkflowFixtureDiagnostics(t *testing.T) {
	for _, subdir := range []string{"examples", "err"} {
		dir, infiles, err := testFindAllWorkflowsInDir(subdir)
		if err != nil {
			panic(err)
		}

		proj := workspace.Identity(dir)

		shellcheck := ""
		if p, err := execabs.LookPath("shellcheck"); err == nil {
			shellcheck = p
		}

		for _, infile := range infiles {
			base := strings.TrimSuffix(infile, filepath.Ext(infile))
			testName := filepath.Base(base)
			t.Run(subdir+"/"+testName, func(t *testing.T) {
				t.Log("Linting workflow file", infile)
				b, err := os.ReadFile(infile)
				if err != nil {
					panic(err)
				}

				tool := ""
				if strings.Contains(testName, "shellcheck") {
					if shellcheck == "" {
						t.Skip("ShellCheck required")
					}
					tool = shellcheck
				}
				request := application.Options{Tools: adapters.Options{ShellCheck: tool}.Registrations()}
				requestSources := []workspace.Source{{Path: "test.yaml", Content: b, Project: proj, Config: &repoconfig.Config{}}}
				errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
				if err != nil {
					t.Fatal(err)
				}

				checkErrors(t, base+".out", errs)
			})
		}
	}
}

func TestWorkflowFixturesBatch(t *testing.T) {
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		t.Skipf("shellcheck is not found: %s", err)
	}

	dir, files, err := testFindAllWorkflowsInDir("examples")
	if err != nil {
		panic(err)
	}

	// Root directory must be testdata/examples/ since some workflow assumes it
	proj := workspace.Identity(dir)

	_, fs, err := testFindAllWorkflowsInDir("err")
	if err != nil {
		panic(err)
	}
	files = append(files, fs...)

	request := application.Options{Tools: adapters.Options{ShellCheck: shellcheck}.Registrations()}
	requestSources := testutil.FixtureSources(t, files, proj, &repoconfig.Config{})
	errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
	if err != nil {
		t.Fatal(err)
	}

	// Check each example workflow file caused at least one error
CheckFiles:
	for _, f := range files {
		for _, e := range errs {
			if e.Path == f {
				continue CheckFiles
			}
		}
		if !strings.Contains(f, "pyflakes") && !strings.Contains(f, "shellcheck") {
			t.Errorf("Workflow %q caused no error: %v", f, errs)
		}
	}
}

func TestProjectWorkflowFixtures(t *testing.T) {
	root := filepath.Join("testdata", "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		panic(err)
	}

	for _, info := range entries {
		if !info.IsDir() {
			continue
		}

		name := info.Name()
		t.Run("project/"+name, func(t *testing.T) {
			repo := filepath.Join(root, name)
			t.Log("Linting project at", repo)

			opts := workspace.Options{
				WorkingDir: repo,
			}
			cfg := filepath.Join(repo, "actionlint.yaml")
			if _, err := os.Stat(cfg); err == nil {
				opts.ConfigFile = cfg
			}
			session, err := workspace.New(opts)
			if err != nil {
				t.Fatal(err)
			}

			proj := workspace.Identity(repo)
			selection, err := session.Directory(filepath.Join(repo, "workflows"), proj)
			if err != nil {
				t.Fatal(err)
			}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), application.Options{WorkingDir: session.WorkingDir()}, selection.Sources))
			if err != nil {
				t.Fatal(err)
			}

			checkErrors(t, repo+".out", errs)
		})
	}
}

func formatDiagnostic(d diagmodel.Diagnostic) string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", d.Path, d.Start.Line, d.Start.Column, d.Message, d.Rule)
}
