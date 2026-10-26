//go:build conformance

package conformance_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/conformance"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v4"
)

var conformanceLog string

func TestMain(m *testing.M) {
	status := m.Run()
	if conformanceLog != "" {
		fmt.Print("\n" + conformanceLog)
	}
	os.Exit(status)
}

type upstreamCase struct {
	ID             string
	Input          string
	Reject         bool
	ExpectedErrors []string
	Contexts       map[string]json.RawMessage
	Kind           string
	Filter         []string
	CompletionOnly bool
	ReferenceSkips []string
}

type upstreamResult struct {
	ID             string   `json:"id"`
	InputSHA256    string   `json:"input_sha256"`
	ExpectedReject bool     `json:"expected_reject"`
	ExpectedErrors []string `json:"expected_errors,omitempty"`
	Actual         []string `json:"actual"`
	Difference     bool     `json:"difference"`
	Contract       string   `json:"contract"`
	ReferenceSkips []string `json:"reference_skips,omitempty"`
	Fingerprint    string   `json:"fingerprint,omitempty"`
}

type upstreamBaseline struct {
	Cases map[string][]string `json:"cases"`
}

type upstreamObservationReport struct {
	Sources      []conformance.Source `json:"sources"`
	Observations []upstreamResult     `json:"observations"`
}

// Bind a review to the pinned source, fixture input, and observed behavior.
func upstreamFingerprint(source conformance.Source, result upstreamResult) string {
	result.Fingerprint = ""
	data, err := json.Marshal(struct {
		Source      conformance.Source `json:"source"`
		Observation upstreamResult     `json:"observation"`
	}{source, result})
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestUpstreamConformance(t *testing.T) {
	dir := os.Getenv("ACTIONLINT_CONFORMANCE_DIR")
	if dir == "" {
		dir = ".cache/conformance"
	}
	dir = repositoryPath(t, dir)
	sources, err := conformance.Sources()
	if err != nil {
		t.Fatal(err)
	}
	var baseline upstreamBaseline
	readConformanceJSON(t, "testdata/differences.json", &baseline)
	if len(baseline.Cases) == 0 {
		t.Fatal("diagnostic baseline has no cases")
	}
	seen := map[string]bool{}
	var results []upstreamResult
	for _, source := range sources {
		root, closeSource, err := source.Open(dir)
		if err != nil {
			t.Fatalf("%v; run go run ./internal/conformance/fetch first", err)
		}
		t.Cleanup(func() { _ = closeSource() })
		cases := loadUpstreamCases(t, source.Name, root)
		counts := map[string]int{"languageservices": 1228, "runner": 24, "schemastore": 67, "yaml-test-suite": 402}
		if len(cases) != counts[source.Name] {
			t.Fatalf("source inventory changed: want %d cases, got %d", counts[source.Name], len(cases))
		}
		for i := range cases {
			cases[i].ID = source.Name + "/" + cases[i].ID
			if seen[cases[i].ID] {
				t.Fatalf("duplicate test case %q", cases[i].ID)
			}
			seen[cases[i].ID] = true
		}
		t.Run(source.Name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(strings.TrimPrefix(c.ID, source.Name+"/"), func(t *testing.T) {
					actual := checkUpstreamCase(t, c)
					if len(c.Filter) > 0 {
						actual = slices.DeleteFunc(actual, func(message string) bool {
							return !slices.ContainsFunc(c.Filter, func(word string) bool { return strings.Contains(message, word) })
						})
					}
					contract := "acceptance"
					if len(c.Filter) > 0 {
						contract = "diagnostics containing: " + strings.Join(c.Filter, ", ")
					}
					if c.CompletionOnly {
						contract = "completes without panic or hang"
					}
					known, ok := baseline.Cases[c.ID]
					result := upstreamResult{
						ID: c.ID, ExpectedReject: c.Reject, ExpectedErrors: c.ExpectedErrors, Actual: actual,
						Difference: !c.CompletionOnly && c.Reject != (len(actual) > 0), Contract: contract,
						InputSHA256:    fmt.Sprintf("%x", sha256.Sum256([]byte(c.Input))),
						ReferenceSkips: c.ReferenceSkips,
					}
					result.Fingerprint = upstreamFingerprint(source, result)
					results = append(results, result)
					if !result.Difference {
						if ok {
							t.Error("now agrees with upstream; remove the resolved entry from differences.json")
						}
						return
					}
					if !ok {
						t.Errorf("upstream reject=%t; actionlint diagnostics=%q; upstream diagnostics=%q", c.Reject, actual, c.ExpectedErrors)
						return
					}
					if os.Getenv("ACTIONLINT_CONFORMANCE_STRICT") == "1" {
						t.Error("reference acceptance differs from actionlint")
					}
					if diff := cmp.Diff(known, actual); diff != "" {
						t.Errorf("recorded actionlint diagnostics changed; review whether behavior improved or regressed (-recorded +current):\n%s", diff)
					}
				})
			}
		})
	}
	for id := range baseline.Cases {
		if !seen[id] {
			t.Errorf("known difference refers to a missing case: %s", id)
		}
	}
	observations := upstreamObservationReport{Sources: sources, Observations: results}
	reportPath := os.Getenv("ACTIONLINT_CONFORMANCE_REPORT")
	if reportPath != "" {
		reportPath = repositoryPath(t, reportPath)
	}
	if reportPath != "" {
		writeConformanceJSON(t, reportPath, observations)
	}
	// Regression assertions above depend only on observations and the baseline.
	var assessments []upstreamAssessmentGroup
	readConformanceJSON(t, "testdata/assessments.json", &assessments)
	review, err := assessUpstreamResults(results, baseline.Cases, assessments)
	if err != nil {
		t.Fatal(err)
	}
	conformanceLog = upstreamAssessmentInventory(assessments, review) + fmt.Sprintf("\nReference comparison: %d cases, %d agreements, %d differences; assessments: %d current, %d stale, %d missing\n", review.Score.Cases, review.Score.Agreements, review.Score.Differences, review.Score.Current, review.Score.Stale, review.Score.Missing)
	if reportPath != "" {
		writeConformanceJSON(t, reportPath+".assessments.json", review)
	}
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		writeUpstreamSummary(t, repositoryPath(t, summary), sources, results, baseline.Cases, assessments)
	}
}

func readConformanceJSON(t *testing.T, filename string, value any) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		t.Fatalf("%s: %v", filename, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("%s: trailing JSON content", filename)
	}
}

func writeConformanceJSON(t *testing.T, filename string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadUpstreamCases(t *testing.T, source string, root fs.FS) []upstreamCase {
	t.Helper()
	switch source {
	case "languageservices":
		return slices.Concat(loadUpstreamExpressions(t, root), loadUpstreamWorkflows(t, root), loadLanguageServiceCases(t, root))
	case "runner":
		var cases []upstreamCase
		// These are the fixture-backed Load_* assertions in ActionManifestManagerL0.cs.
		for _, name := range []string{
			"dockerfileaction", "dockerfileaction_init", "dockerfileaction_cleanup", "dockerfileaction_init_default",
			"dockerfileaction_cleanup_default", "dockerfileaction_noargs_noenv_noentrypoint", "dockerfileaction_arg_env_expression",
			"dockerhubaction", "nodeaction", "node16action", "node20action", "node24action", "nodeaction_init",
			"nodeaction_init_default", "nodeaction_cleanup", "nodeaction_cleanup_default", "pluginaction",
			"conditional_composite_action", "composite_action_without_using_token", "dockerfileaction_env_invalid_context",
		} {
			file := "src/Test/TestData/" + name + ".yml"
			reject := name == "composite_action_without_using_token" || name == "dockerfileaction_env_invalid_context"
			cases = append(cases, upstreamCase{ID: file, Input: readUpstream(t, root, file), Kind: "action", Reject: reject})
		}
		return append(cases, loadRunnerExpressions(t, root)...)
	case "schemastore":
		var cases []upstreamCase
		for _, kind := range []string{"action", "workflow"} {
			for _, suite := range []string{"test", "negative_test"} {
				pattern := "src/" + suite + "/github-" + kind + "/*"
				for _, file := range upstreamFiles(t, root, pattern) {
					cases = append(cases, upstreamCase{ID: file, Input: readUpstream(t, root, file), Kind: kind, Reject: suite == "negative_test"})
				}
			}
		}
		return cases
	case "yaml-test-suite":
		var cases []upstreamCase
		if err := fs.WalkDir(root, ".", func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && path.Base(file) == "in.yaml" {
				_, err := fs.Stat(root, path.Join(path.Dir(file), "error"))
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				cases = append(cases, upstreamCase{ID: file, Input: readUpstream(t, root, file), Kind: "yaml", Reject: err == nil})
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return cases
	default:
		t.Fatalf("no adapter for source %q", source)
		return nil
	}
}

func readUpstream(t *testing.T, root fs.FS, file string) string {
	t.Helper()
	b, err := fs.ReadFile(root, file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func upstreamFiles(t *testing.T, root fs.FS, pattern string) []string {
	t.Helper()
	files, err := fs.Glob(root, pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no upstream files match %q", pattern)
	}
	return files
}

func loadUpstreamExpressions(t *testing.T, root fs.FS) []upstreamCase {
	t.Helper()
	var cases []upstreamCase
	for _, file := range upstreamFiles(t, root, "expressions/testdata/*.json") {
		var groups map[string][]struct {
			Expr     string
			Contexts map[string]json.RawMessage
			Err      *struct{ Kind, Value string }
		}
		if err := json.Unmarshal([]byte(readUpstream(t, root, file)), &groups); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, group := range slices.Sorted(maps.Keys(groups)) {
			for i, entry := range groups[group] {
				c := upstreamCase{ID: fmt.Sprintf("%s/%s/%d", file, group, i), Input: entry.Expr, Kind: "expression", Contexts: entry.Contexts}
				if entry.Err != nil && entry.Err.Kind != "evaluation" {
					c.Reject = true
					c.ExpectedErrors = []string{entry.Err.Value}
				}
				cases = append(cases, c)
			}
		}
	}
	return cases
}

var upstreamDocumentBoundary = regexp.MustCompile(`\r?\n---\r?\n`)

func loadUpstreamWorkflows(t *testing.T, root fs.FS) []upstreamCase {
	t.Helper()
	var cases []upstreamCase
	for _, file := range upstreamFiles(t, root, "workflow-parser/testdata/reader/*.yml") {
		docs := upstreamDocumentBoundary.Split(readUpstream(t, root, file), -1)
		if len(docs) < 3 {
			t.Fatalf("%s has fewer than three fixture documents", file)
		}
		var metadata struct {
			Skip []string `yaml:"skip"`
		}
		if err := yaml.Unmarshal([]byte(docs[0]), &metadata); err != nil {
			t.Fatalf("%s: invalid fixture metadata: %v", file, err)
		}
		var expected struct{ Errors []struct{ Message string } }
		if err := json.Unmarshal([]byte(docs[len(docs)-1]), &expected); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		c := upstreamCase{ID: file, Input: docs[1], Kind: "workflow", Reject: len(expected.Errors) > 0, ReferenceSkips: metadata.Skip}
		for _, e := range expected.Errors {
			c.ExpectedErrors = append(c.ExpectedErrors, e.Message)
		}
		cases = append(cases, c)
	}
	return cases
}

func checkUpstreamCase(t *testing.T, c upstreamCase) []string {
	t.Helper()
	switch c.Kind {
	case "expression":
		parser := exprlang.Parser{}
		lexer := exprlang.NewLexer(c.Input + "}}")
		node, err := parser.Parse(lexer)
		if err != nil {
			return []string{err.Error()}
		}
		if lexer.Offset() != len(c.Input)+2 {
			return []string{fmt.Sprintf("unexpected expression terminator at byte %d in standalone expression", lexer.Offset()-2)}
		}
		checker := exprlang.NewChecker(false, nil)
		contexts := map[string]exprlang.Type{}
		for name := range c.Contexts {
			contexts[strings.ToLower(name)] = exprlang.AnyType{}
		}
		checker.SetContextTypes(contexts)
		checker.SetContextAvailability(slices.Sorted(maps.Keys(contexts)))
		var diagnostics []*exprlang.Error
		// GitHub's parser checks symbols and arity, but leaves coercion and evaluation to its evaluator.
		exprlang.Visit(node, func(n, _ exprlang.Node, entering bool) {
			if !entering {
				return
			}
			switch n := n.(type) {
			case *exprlang.VariableNode:
				_, errs := checker.Check(n)
				diagnostics = append(diagnostics, errs...)
			case *exprlang.FuncCallNode:
				callChecker := exprlang.NewChecker(false, nil)
				callChecker.SetContextTypes(map[string]exprlang.Type{"argument": exprlang.AnyType{}})
				callChecker.SetContextAvailability([]string{"argument"})
				callChecker.SetSpecialFunctionAvailability([]string{"always", "cancelled", "failure", "success", "hashfiles"})
				args := make([]exprlang.Node, len(n.Args))
				for i := range args {
					argument, err := parser.Parse(exprlang.NewLexer("argument}}"))
					if err != nil {
						t.Fatal(err)
					}
					*argument.Token() = *n.Args[i].Token()
					args[i] = argument
				}
				call := *n
				call.Args = args
				_, errs := callChecker.Check(&call)
				diagnostics = append(diagnostics, errs...)
			}
		})
		out := []string{}
		for _, err := range diagnostics {
			out = append(out, err.Error())
		}
		return out
	case "workflow":
		result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "<stdin>", Content: []byte(c.Input), Config: &repoconfig.Config{}}})
		if err != nil {
			t.Fatal(err)
		}
		errs := make([]*findingmodel.Finding, 0, len(result.Diagnostics))
		for _, d := range result.Diagnostics {
			errs = append(errs, &findingmodel.Finding{Diagnostic: d})
		}
		return upstreamDiagnostics(errs)
	case "action":
		var meta actionmeta.Metadata
		if err := codec.Unmarshal([]byte(c.Input), &meta); err != nil {
			return []string{err.Error()}
		}
		root := t.TempDir()
		dir := filepath.Join(root, "action")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "action.yml"), []byte(c.Input), 0o644); err != nil {
			t.Fatal(err)
		}
		// Manifest-parser fixtures assume companion files exist; their contents are never executed.
		files := []string{meta.Runs.Main, meta.Runs.Pre, meta.Runs.Post, meta.Runs.PreEntrypoint, meta.Runs.Entrypoint, meta.Runs.PostEntrypoint}
		if !strings.HasPrefix(meta.Runs.Image, "docker://") {
			files = append(files, meta.Runs.Image)
		}
		for _, file := range files {
			if file == "" {
				continue
			}
			if !filepath.IsLocal(file) {
				t.Fatalf("fixture references non-local companion file %q", file)
			}
			file = filepath.Join(dir, filepath.FromSlash(file))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		metadata, err := actionmeta.Parse(filepath.Join(dir, "action.yml"), []byte(c.Input))
		if err != nil {
			t.Fatal(err)
		}
		// Input binding belongs to invocation checking; these fixtures test the manifest contract.
		out := upstreamDiagnostics(actioncheck.Check(metadata, &workflowlang.Pos{Line: 1, Col: 1}))
		quotedDir := strings.TrimSuffix(strings.TrimPrefix(strconv.Quote(dir), `"`), `"`)
		for i := range out {
			out[i] = strings.ReplaceAll(out[i], filepath.ToSlash(dir), "<action>")
			out[i] = strings.ReplaceAll(out[i], quotedDir, "<action>")
			out[i] = strings.ReplaceAll(out[i], `<action>\\action.yml`, "<action>/action.yml")
		}
		return out
	case "yaml":
		decoder := yaml.NewDecoder(bytes.NewBufferString(c.Input))
		for {
			var document yaml.Node
			err := decoder.Decode(&document)
			if errors.Is(err, io.EOF) {
				return []string{}
			}
			if err != nil {
				return []string{err.Error()}
			}
		}
	default:
		t.Fatalf("unknown conformance case kind %q", c.Kind)
		return nil
	}
}

func upstreamDiagnostics(errs []*findingmodel.Finding) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		err.Path = filepath.ToSlash(err.Path)
		out = append(out, err.String())
	}
	return out
}

func repositoryPath(t *testing.T, name string) string {
	t.Helper()
	if filepath.IsAbs(name) {
		return name
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, name)
}
