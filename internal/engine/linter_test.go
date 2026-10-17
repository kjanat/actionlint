package engine

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/render"
	"actionlint.kjanat.dev/internal/tools/adapters"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"

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
		request := Request{}
		var lConfig *repoconfig.Config
		var err error

		lConfig = &repoconfig.Config{}
		source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + tc.steps
		request.Sources = []SourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: proj, Config: lConfig}}
		errs, err := testFindings(runTestAnalysis(t.Context(), request))
		if err != nil || len(errs) != tc.want {
			t.Fatalf("got %v, %v; want %d diagnostics", errs, err, tc.want)
		}
		for i, diagnostic := range errs {
			if diagnostic.Rule != "missing-local-action" || diagnostic.Start.Line != 6+i || !strings.Contains(diagnostic.Message, "has no action.yml or action.yaml") {
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
			request := Request{}
			lConfig := cfg
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./missing\n      - uses: invalid\n"
			request.Sources = []SourceUnit{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: workspace.Identity(t.TempDir()), Config: lConfig}}
			findings, err := testFindings(runTestAnalysis(t.Context(), request))
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

			request := Request{Tools: adapters.Options{ShellCheck: shellcheck}.Registrations(), Sources: readFixtureSources(t, []string{f}, proj, &repoconfig.Config{})}
			errs, err := testFindings(runTestAnalysis(t.Context(), request))
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

func checkErrors(t *testing.T, outfile string, errs []*findingmodel.Finding) {
	t.Helper()
	t.Logf("Checking %d errors with %q", len(errs), outfile)
	for i, err := range errs {
		t.Logf("  Error[%d]: %s", i, err.String())
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

	slices.SortFunc(errs, findingmodel.Compare)

	if len(errs) != len(expected) {
		ms := make([]string, 0, len(errs))
		for _, err := range errs {
			ms = append(ms, err.String())
		}
		t.Fatalf("%d errors are expected but actually got %d errors:\n%v", len(expected), len(errs), strings.Join(ms, "\n"))
	}

	for i := range errs {
		errs[i].Path = filepath.ToSlash(errs[i].Path) // For Windows
		want, have := expected[i], errs[i].String()
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
				request := Request{Tools: adapters.Options{ShellCheck: tool}.Registrations(), Sources: []SourceUnit{{Path: "test.yaml", Content: b, Project: proj, Config: &repoconfig.Config{}}}}
				errs, err := testFindings(runTestAnalysis(t.Context(), request))
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

	request := Request{Tools: adapters.Options{ShellCheck: shellcheck}.Registrations(), Sources: readFixtureSources(t, files, proj, &repoconfig.Config{})}
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
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
			errs, err := testFindings(runTestAnalysis(t.Context(), Request{WorkingDir: session.WorkingDir(), Sources: selection.Sources}))
			if err != nil {
				t.Fatal(err)
			}

			checkErrors(t, repo+".out", errs)
		})
	}
}

type customRuleForTest struct {
	workflowvisit.Base
	count int
}

func (r *customRuleForTest) VisitStep(n *workflowlang.Step) error {
	r.count++
	if r.count > 1 {
		r.Errorf(n.Pos, "only single step is allowed but got %d steps", r.count)
	}
	return nil
}

func TestTraversalAddsCustomRule(t *testing.T) {
	request := Request{
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			r := &customRuleForTest{
				Base: workflowvisit.NewBase("this-is-test", ""),
			}
			return append(rules, r)
		},
	}

	{
		w := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
`
		request.Sources = []SourceUnit{{Path: "test.yaml", Content: []byte(w), Config: &repoconfig.Config{}}}
		errs, err := testFindings(runTestAnalysis(t.Context(), request))
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 0 {
			t.Fatal("wanted no error but have", errs)
		}
	}

	{
		w := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
      - run: echo
`
		request.Sources = []SourceUnit{{Path: "test.yaml", Content: []byte(w), Config: &repoconfig.Config{}}}
		errs, err := testFindings(runTestAnalysis(t.Context(), request))
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 1 {
			t.Fatal("wanted 1 error but have", errs)
		}

		var b strings.Builder
		render.FindingOf(errs[0].Diagnostic, nil).PrettyPrint(&b)
		have := b.String()
		want := "test.yaml:7:9: only single step is allowed but got 2 steps [this-is-test]\n"
		if have != want {
			t.Fatalf("wanted error message %q but have %q", want, have)
		}
	}
}

func TestTraversalRemovesBuiltinRule(t *testing.T) {
	request := Request{
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			for i, r := range rules {
				if r.Name() == "runner-label" {
					rules = append(rules[:i], rules[i+1:]...)
					break
				}
			}
			return rules
		},
	}

	f := filepath.Join("testdata", "err", "invalid_runner_labels.yaml")
	request.Sources = readFixtureSources(t, []string{f}, nil, &repoconfig.Config{})
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatal("no error was expected because runner-label rule was removed but got:", errs)
	}
}

func BenchmarkLintWorkflowContent(b *testing.B) {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	proj := workspace.Identity(dir)

	// Measure performance of traversing with checks except for external process rules (shellcheck, pyflakes)
	// Reading file content is not included in benchmark measurement.

	for _, name := range []string{"minimal", "small", "large"} {
		var f string
		switch name {
		case "minimal":
			f = filepath.Join(dir, "testdata", "bench", "minimal.yaml")
		case "small":
			f = filepath.Join(dir, "testdata", "bench", "small.yaml")
		case "large":
			f = filepath.Join(dir, "testdata", "bench", "many_scripts.yaml")
		}
		content, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(name, func(b *testing.B) {
			for range b.N {
				result, err := runTestAnalysis(b.Context(), Request{WorkingDir: dir, Sources: []SourceUnit{{Path: f, Content: content, Project: proj, Config: &repoconfig.Config{}}}})
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Diagnostics) > 0 {
					b.Fatal("some error occurred:", result.Diagnostics)
				}
			}
		})
	}
}
