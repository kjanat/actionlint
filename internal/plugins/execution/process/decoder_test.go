package process_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"github.com/google/go-cmp/cmp"
)

func diagnostic(filename, code, message string, row, column int) checkinputs.ScriptDiagnostic {
	return checkinputs.ScriptDiagnostic{Filename: filename, Code: code, Message: message, Location: checkinputs.ScriptPosition{Row: row, Column: column}}
}

func TestPointDiagnosticJSONOmitsEndLocation(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(diagnostic("a.py", "E1", "point", 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if _, present := value["end_location"]; present {
		t.Errorf("point diagnostic has zero end_location: %s", encoded)
	}
	ranged := diagnostic("a.py", "E1", "range", 1, 1)
	ranged.EndLocation = checkinputs.ScriptPosition{Row: 1, Column: 2}
	encoded, err = json.Marshal(ranged)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if _, present := value["end_location"]; !present {
		t.Errorf("ranged diagnostic omitted end_location: %s", encoded)
	}
}

func TestEnvelopeDecode(t *testing.T) {
	t.Parallel()
	stdout := `{"ok":false,"diagnostics":[{"filename":"actionlint.sh","code":"X1","message":"m","severity":"warning","location":{"row":2,"column":3},"end_location":{"row":2,"column":5},"template_effect":"opaque-name"}],"errors":["partial analysis"]}`
	got, err := process.Envelope{}.Decode(t.Context(), []byte(stdout), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := diagnostic("actionlint.sh", "X1", "m", 2, 3)
	want.Severity, want.EndLocation, want.TemplateEffect = "warning", checkinputs.ScriptPosition{Row: 2, Column: 5}, checkinputs.TemplateEffectOpaqueName
	if diff := cmp.Diff(process.Result{Diagnostics: []checkinputs.ScriptDiagnostic{want}, Errors: []string{"partial analysis"}}, got); diff != "" {
		t.Errorf("result (-want +got):\n%s", diff)
	}
}

func TestEnvelopeFailureWithoutExplanation(t *testing.T) {
	t.Parallel()
	got, err := process.Envelope{}.Decode(t.Context(), []byte(`{"ok":false}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tool reported failure without an explanation"}; !cmp.Equal(want, got.Errors) {
		t.Errorf("errors = %q, want %q", got.Errors, want)
	}
}

func TestEnvelopeRejects(t *testing.T) {
	t.Parallel()
	tooMany := `{"ok":true,"diagnostics":[` + strings.TrimSuffix(strings.Repeat(`{},`, process.MaxDiagnostics+1), ",") + `]}`
	for name, stdout := range map[string]string{
		"missing ok":     `{"diagnostics":[]}`,
		"duplicate key":  `{"ok":true,"ok":false}`,
		"not JSON":       `ok`,
		"too many":       tooMany,
		"too much input": `{"ok":true,"errors":["` + strings.Repeat("x", process.MaxOutputBytes) + `"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := (process.Envelope{}).Decode(t.Context(), []byte(stdout), nil); err == nil {
				t.Error("decoded invalid envelope")
			}
		})
	}
}

// testdata/ruff.sarif is Ruff 0.17 `check --output-format sarif` output.
func TestSARIFDecodeRuffCapture(t *testing.T) {
	t.Parallel()
	stdout, err := os.ReadFile("testdata/ruff.sarif")
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"filename": "actionlint.py", "filename_abs": "C:/Users/kjanat/AppData/Local/Temp/provider-capture/actionlint.py"}
	got, err := process.SARIF{}.Decode(t.Context(), stdout, vars)
	if err != nil {
		t.Fatal(err)
	}
	finding := func(code, message, url string, row, column, endColumn int) checkinputs.ScriptDiagnostic {
		d := diagnostic("actionlint.py", code, message, row, column)
		d.Severity, d.URL, d.EndLocation = "error", url, checkinputs.ScriptPosition{Row: row, Column: endColumn}
		return d
	}
	const unused = "https://docs.astral.sh/ruff/rules/unused-import"
	want := process.Result{Diagnostics: []checkinputs.ScriptDiagnostic{
		finding("E401", "Multiple imports on one line", "https://docs.astral.sh/ruff/rules/multiple-imports-on-one-line", 1, 1, 15),
		finding("F401", "`os` imported but unused", unused, 1, 8, 10),
		finding("F401", "`sys` imported but unused", unused, 1, 12, 15),
		finding("F401", "`json` imported but unused", unused, 2, 8, 12),
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("result (-want +got):\n%s", diff)
	}
}

// testdata/edge.sarif is hand-written: rule lookup by index, a finding in a
// followed file, a suppressed result and a passing result.
func TestSARIFDecodeEdges(t *testing.T) {
	t.Parallel()
	stdout, err := os.ReadFile("testdata/edge.sarif")
	if err != nil {
		t.Fatal(err)
	}
	got, err := process.SARIF{}.Decode(t.Context(), stdout, map[string]string{"filename": "actionlint.sh"})
	if err != nil {
		t.Fatal(err)
	}
	byIndex := diagnostic("actionlint.sh", "EX2", "rule found by index", 2, 1)
	byIndex.Name, byIndex.Severity = "second-rule", "warning"
	followed := diagnostic("lib/common.sh", "EX1", "finding in a followed file", 4, 3)
	followed.Name, followed.Severity, followed.URL, followed.Source = "first-rule", "note", "https://example.invalid/EX1", "lib/common.sh"
	followed.EndLocation = checkinputs.ScriptPosition{Row: 4, Column: 9}
	if diff := cmp.Diff(process.Result{Diagnostics: []checkinputs.ScriptDiagnostic{byIndex, followed}}, got); diff != "" {
		t.Errorf("result (-want +got):\n%s", diff)
	}
}

func TestSARIFNativeSuppressionDecisions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, suppressions string
		count              int
		invalid            bool
	}{
		{"unspecified", `null`, 1, false},
		{"empty", `[]`, 1, false},
		{"in source", `[{"kind":"inSource"}]`, 0, false},
		{"accepted", `[{"kind":"external","status":"accepted"}]`, 0, false},
		{"under review", `[{"kind":"inSource","status":"underReview"}]`, 1, false},
		{"rejected", `[{"kind":"inSource","status":"rejected"}]`, 1, false},
		{"null status", `[{"kind":"inSource","status":null}]`, 0, false},
		{"one accepted", `[{"kind":"external","status":"rejected"},{"kind":"inSource","status":"accepted"}]`, 0, false},
		{"not array", `{}`, 0, true},
		{"missing kind", `[{}]`, 0, true},
		{"unknown kind", `[{"kind":"mystery"}]`, 0, true},
		{"unknown status", `[{"kind":"inSource","status":"mystery"}]`, 0, true},
		{"empty status", `[{"kind":"inSource","status":""}]`, 0, true},
		{"invalid status", `[{"kind":"inSource","status":false}]`, 0, true},
		{"invalid after accepted", `[{"kind":"inSource","status":"accepted"},{"kind":"invalid"}]`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout := `{"version":"2.1.0","runs":[{"results":[{"ruleId":"X1","message":{"text":"finding"},"suppressions":` + tc.suppressions + `,"locations":[{"physicalLocation":{"artifactLocation":{"uri":"workflow.yml"},"region":{"startLine":1}}}]}]}]}`
			got, err := (process.SARIF{}).Decode(t.Context(), []byte(stdout), map[string]string{"filename": "workflow.yml"})
			if tc.invalid {
				if err == nil {
					t.Fatal("malformed native suppression accepted")
				}
				return
			}
			if err != nil || len(got.Diagnostics) != tc.count {
				t.Fatalf("got %+v, %v; want %d active findings", got, err, tc.count)
			}
		})
	}
}

func TestSARIFRejects(t *testing.T) {
	t.Parallel()
	location := `"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a"},"region":{"startLine":1}}}]`
	for name, stdout := range map[string]string{
		"version":     `{"version":"2.0.0","runs":[]}`,
		"no rule":     `{"version":"2.1.0","runs":[{"results":[{"message":{"text":"m"},` + location + `}]}]}`,
		"no message":  `{"version":"2.1.0","runs":[{"results":[{"ruleId":"X",` + location + `}]}]}`,
		"no location": `{"version":"2.1.0","runs":[{"results":[{"ruleId":"X","message":{"text":"m"}}]}]}`,
		"no line":     `{"version":"2.1.0","runs":[{"results":[{"ruleId":"X","message":{"text":"m"},"locations":[{"physicalLocation":{"region":{"charOffset":3}}}]}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := (process.SARIF{}).Decode(t.Context(), []byte(stdout), nil); err == nil {
				t.Error("decoded invalid SARIF")
			}
		})
	}
}

func decodeJQ(t *testing.T, ctx context.Context, program string, input process.JQInput, stdout string) (process.Result, error) {
	t.Helper()
	decoder, err := process.NewJQ(program, input)
	if err != nil {
		t.Fatal(err)
	}
	return decoder.Decode(ctx, []byte(stdout), map[string]string{"filename": "actionlint.sh"})
}

func TestJQDecodesBothResultShapes(t *testing.T) {
	t.Parallel()
	const one = `{filename: $vars.filename, code: "X1", message: ., location: {row: 1, column: 1}}`
	got, err := decodeJQ(t, t.Context(), "["+one+"]", process.JQText, "plain text")
	if err != nil {
		t.Fatal(err)
	}
	want := process.Result{Diagnostics: []checkinputs.ScriptDiagnostic{diagnostic("actionlint.sh", "X1", "plain text", 1, 1)}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("array result (-want +got):\n%s", diff)
	}
	got, err = decodeJQ(t, t.Context(), `{diagnostics: [`+one+`], sources: ["lib.sh"], errors: ["partial"]}`, process.JQText, "plain text")
	if err != nil {
		t.Fatal(err)
	}
	want.Sources, want.Errors = []string{"lib.sh"}, []string{"partial"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("object result (-want +got):\n%s", diff)
	}
}

// A transform sees no ambient environment, even variables the host has.
func TestJQEnvironmentIsEmpty(t *testing.T) {
	t.Setenv("ACTIONLINT_JQ_SANDBOX_PROBE", "leaked")
	for _, expression := range []string{"$ENV", "env"} {
		program := `[{filename: "f", code: "X", message: (` + expression + ` | tojson), location: {row: 1, column: 1}}]`
		got, err := decodeJQ(t, t.Context(), program, process.JQJSON, `null`)
		if err != nil {
			t.Fatal(err)
		}
		if message := got.Diagnostics[0].Message; message != "{}" {
			t.Errorf("%s = %s, want {}", expression, message)
		}
	}
}

func TestJQRefusesModulesAndInputs(t *testing.T) {
	t.Parallel()
	for _, program := range []string{
		`import "lib" as lib; .`,
		`import "data" as $data; .`,
		`include "lib"; .`,
		`input`,
		`[inputs]`,
		`debug`,
		`stderr`,
	} {
		_, err := process.NewJQ(program, process.JQJSON)
		if err == nil {
			t.Errorf("compiled %q", program)
			continue
		}
		t.Logf("%s: %v", program, err)
	}
}

func TestJQHonoursDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := decodeJQ(t, ctx, `last(repeat(1))`, process.JQJSON, `null`)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("transform stopped after %s", elapsed)
	}
}

func TestJQRejects(t *testing.T) {
	t.Parallel()
	const item = `{filename: "f", code: "X", message: "m", location: {row: 1, column: 1}}`
	for name, program := range map[string]string{
		"no value":           `empty`,
		"two values":         `[], []`,
		"halt":               `"stop" | halt_error`,
		"runtime error":      `error("boom")`,
		"wrong shape":        `{}`,
		"unknown member":     `[` + item + ` + {extra: 1}]`,
		"template effect":    `[` + item + ` + {template_effect: "opaque-name"}]`,
		"missing location":   `[` + item + ` | del(.location)]`,
		"zero row":           `[` + item + ` | .location.row = 0]`,
		"empty code":         `[` + item + ` | .code = ""]`,
		"too many":           `[range(0; ` + strconv.Itoa(process.MaxDiagnostics+1) + `) | ` + item + `]`,
		"output over limits": `[range(0; 300) | ` + item + ` | .message = ("x" * 30000)]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeJQ(t, t.Context(), program, process.JQJSON, `null`)
			if err == nil {
				t.Fatal("accepted invalid transform output")
			}
			t.Log(err)
		})
	}
}

func TestJQRejectsOversizedInput(t *testing.T) {
	t.Parallel()
	decoder, err := process.NewJQ(`[]`, process.JQText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Decode(t.Context(), bytes.Repeat([]byte("x"), process.MaxOutputBytes+1), nil); err == nil {
		t.Error("accepted stdout over the limit")
	}
}

func TestNewJQRejectsUnknownInput(t *testing.T) {
	t.Parallel()
	if _, err := process.NewJQ(`.`, process.JQInput("yaml")); err == nil {
		t.Error("accepted unknown input format")
	}
}
