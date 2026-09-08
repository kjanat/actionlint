//go:build conformance

package actionlint

import (
	"bytes"
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

	"actionlint.kjanat.dev/internal/conformance"
	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v4"
)

type upstreamCase struct {
	ID             string
	Input          string
	Reject         bool
	ExpectedErrors []string
	Contexts       map[string]json.RawMessage
	Kind           string
	Filter         []string
	CompletionOnly bool
}

type upstreamResult struct {
	ID              string   `json:"id"`
	ExpectedReject  bool     `json:"expected_reject"`
	ExpectedErrors  []string `json:"expected_errors,omitempty"`
	Actual          []string `json:"actual"`
	Difference      bool     `json:"difference"`
	Contract        string   `json:"contract"`
	KnownDifference string   `json:"known_difference,omitempty"`
}

type upstreamDifference struct {
	Reason string   `json:"reason"`
	Actual []string `json:"actual"`
}

type upstreamDifferenceGroup struct {
	Reason string              `json:"reason"`
	Cases  map[string][]string `json:"cases"`
}

func TestUpstreamConformance(t *testing.T) {
	dir := os.Getenv("ACTIONLINT_CONFORMANCE_DIR")
	if dir == "" {
		dir = ".cache/conformance"
	}
	sources, err := conformance.Sources()
	if err != nil {
		t.Fatal(err)
	}
	baseline := map[string]upstreamDifference{}
	data, err := os.ReadFile("testdata/conformance/differences.json")
	if err != nil {
		t.Fatal(err)
	}
	var groups []upstreamDifferenceGroup
	if err := json.Unmarshal(data, &groups); err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if strings.TrimSpace(group.Reason) == "" || len(group.Cases) == 0 {
			t.Fatal("difference group must have a reason and at least one case")
		}
		for id, actual := range group.Cases {
			if _, ok := baseline[id]; ok {
				t.Fatalf("duplicate known difference %q", id)
			}
			baseline[id] = upstreamDifference{group.Reason, actual}
		}
	}
	seen := map[string]bool{}
	var results []upstreamResult
	for _, source := range sources {
		root, closeSource, err := source.Open(dir)
		if err != nil {
			t.Fatalf("%v; run go run ./scripts/fetch-conformance first", err)
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
					known, ok := baseline[c.ID]
					result := upstreamResult{c.ID, c.Reject, c.ExpectedErrors, actual, !c.CompletionOnly && c.Reject != (len(actual) > 0), contract, known.Reason}
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
						t.Errorf("upstream difference: %s", known.Reason)
					}
					if diff := cmp.Diff(known.Actual, actual); diff != "" {
						t.Errorf("known difference changed (-want +got):\n%s", diff)
					}
					t.Logf("UPSTREAM DIFFERENCE: %s", known.Reason)
				})
			}
		})
	}
	for id := range baseline {
		if !seen[id] {
			t.Errorf("known difference refers to a missing case: %s", id)
		}
	}
	counts := map[string]int{}
	for _, result := range results {
		if result.Difference {
			counts["differences"]++
		} else {
			counts["agreements"]++
		}
	}
	t.Logf("Upstream comparison: %d cases, %d agreements, %d differences", len(results), counts["agreements"], counts["differences"])
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		writeUpstreamSummary(t, summary, sources, results)
	}
	if report := os.Getenv("ACTIONLINT_CONFORMANCE_REPORT"); report != "" {
		data, err := json.MarshalIndent(struct {
			Sources []conformance.Source `json:"sources"`
			Results []upstreamResult     `json:"results"`
		}{sources, results}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(report, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeUpstreamSummary(t *testing.T, filename string, sources []conformance.Source, results []upstreamResult) {
	t.Helper()
	var text strings.Builder
	text.WriteString("## Upstream conformance\n\nAgreement means matching acceptance for the stated contract, not full runtime compatibility. Known differences are executed and checked against their recorded diagnostics.\n\n")
	text.WriteString("| Source | Cases | Agreements | Known differences | New differences |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, source := range sources {
		var total, agree, known, unknown int
		for _, result := range results {
			if !strings.HasPrefix(result.ID, source.Name+"/") {
				continue
			}
			total++
			switch {
			case !result.Difference:
				agree++
			case result.KnownDifference != "":
				known++
			default:
				unknown++
			}
		}
		fmt.Fprintf(&text, "| [%s](https://github.com/%s/tree/%s) | %d | %d | %d | %d |\n", source.Name, source.Repository, source.Revision, total, agree, known, unknown)
	}
	text.WriteString("\nThe JSON artifact includes each case ID, upstream expectation, actionlint diagnostics, and explanation for known differences. A changed or resolved known difference also fails the test.\n")
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(text.String())
	closeErr := f.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
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
		var expected struct{ Errors []struct{ Message string } }
		if err := json.Unmarshal([]byte(docs[len(docs)-1]), &expected); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		c := upstreamCase{ID: file, Input: docs[1], Kind: "workflow", Reject: len(expected.Errors) > 0}
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
		parser := ExprParser{}
		lexer := NewExprLexer(c.Input + "}}")
		node, err := parser.Parse(lexer)
		if err != nil {
			return []string{err.Error()}
		}
		if lexer.Offset() != len(c.Input)+2 {
			return []string{fmt.Sprintf("unexpected expression terminator at byte %d in standalone expression", lexer.Offset()-2)}
		}
		checker := NewExprSemanticsChecker(false, nil)
		checker.vars = map[string]ExprType{}
		for name := range c.Contexts {
			checker.vars[strings.ToLower(name)] = AnyType{}
		}
		checker.SetContextAvailability(slices.Sorted(maps.Keys(checker.vars)))
		// GitHub's parser checks symbols and arity, but leaves coercion and evaluation to its evaluator.
		VisitExprNode(node, func(n, _ ExprNode, entering bool) {
			if !entering {
				return
			}
			switch n := n.(type) {
			case *VariableNode:
				checker.checkVariable(n)
			case *FuncCallNode:
				callChecker := NewExprSemanticsChecker(false, nil)
				callChecker.vars = map[string]ExprType{"argument": AnyType{}}
				callChecker.SetContextAvailability([]string{"argument"})
				callChecker.SetSpecialFunctionAvailability([]string{"always", "cancelled", "failure", "success", "hashfiles"})
				args := make([]ExprNode, len(n.Args))
				for i := range args {
					args[i] = &VariableNode{Name: "argument", tok: n.Args[i].Token()}
				}
				_, errs := callChecker.Check(&FuncCallNode{Callee: n.Callee, Args: args, tok: n.tok})
				checker.errs = append(checker.errs, errs...)
			}
		})
		out := []string{}
		for _, err := range checker.errs {
			out = append(out, err.Error())
		}
		return out
	case "workflow":
		linter, err := NewLinter(io.Discard, &LinterOptions{})
		if err != nil {
			t.Fatal(err)
		}
		linter.defaultConfig = &Config{}
		errs, err := linter.Lint("<stdin>", []byte(c.Input), nil)
		if err != nil {
			t.Fatal(err)
		}
		return upstreamDiagnostics(errs)
	case "action":
		var meta ActionMetadata
		if err := yaml.Unmarshal([]byte(c.Input), &meta); err != nil {
			return []string{err.Error()}
		}
		meta.file, meta.src = "action.yml", []byte(c.Input)
		meta.dir = t.TempDir()
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
			file = filepath.Join(meta.dir, filepath.FromSlash(file))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		rule := NewRuleAction(nil)
		rule.checkLocalActionMetadata(&meta, &ExecAction{Uses: &String{Value: "./action", Pos: &Pos{Line: 1, Col: 1}}})
		out := upstreamDiagnostics(rule.Errs())
		quotedDir := strings.TrimSuffix(strings.TrimPrefix(strconv.Quote(meta.dir), `"`), `"`)
		for i := range out {
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

func upstreamDiagnostics(errs []*Error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		err.Filepath = filepath.ToSlash(err.Filepath)
		out = append(out, err.Error())
	}
	return out
}
