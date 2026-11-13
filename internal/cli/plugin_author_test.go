package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestPluginTestUsesHostPipelineAndReportsAssertions(t *testing.T) {
	t.Parallel()
	manifest := authoringTestManifest(t)
	input, err := json.Marshal(map[string]any{
		"manifest": manifest,
		"cases": []any{
			map[string]any{
				"name": "config", "request": map[string]any{"filename": "a.py", "source": "x", "config": map[string]any{"a": 1}},
				"variables": map[string]string{"kept": "yes"}, "expect_config": map[string]any{"a": 1, "defaulted": true},
				"expect_vars": map[string]string{"filename": "a.py", "filename_abs": filepath.Join(mustAbs(t, "."), "a.py"), "kept": "yes", "added": "yes"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	command := Command{Stdin: bytes.NewReader(input), Stdout: &stdout, Stderr: &stderr}
	if status := command.Main([]string{"actionlint", "plugin", "test", "--json"}); status != 0 {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	var report struct {
		Valid bool `json:"valid"`
		Cases []struct {
			Passed bool              `json:"passed"`
			Config json.RawMessage   `json:"config"`
			Vars   map[string]string `json:"variables"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Valid || len(report.Cases) != 1 || !report.Cases[0].Passed || report.Cases[0].Vars["added"] != "yes" {
		t.Fatalf("report=%s", stdout.String())
	}
}

func TestPluginTestRejectsMalformedFixturesAndWrongExpectations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		want  string
	}{
		{`{"manifest":{},"cases":[{"name":"x","unknown":true}]}`, "unknown field"},
		{`{"manifest":{},"cases":[{"name":"x","request":{"filename":"a"},"expect_config":{}}]}`, "manifest"},
	} {
		var stdout, stderr bytes.Buffer
		command := Command{Stdin: strings.NewReader(tc.input), Stdout: &stdout, Stderr: &stderr}
		if status := command.Main([]string{"actionlint", "plugin", "test"}); status != 1 || !strings.Contains(stdout.String(), tc.want) {
			t.Errorf("status=%d stdout=%s stderr=%s want=%q", status, stdout.String(), stderr.String(), tc.want)
		}
	}
}

func TestPluginTestDistinguishesExpectedPipelineErrorFromMismatch(t *testing.T) {
	t.Parallel()
	manifest := authoringTestManifest(t)
	input, err := json.Marshal(map[string]any{
		"manifest": manifest,
		"cases": []any{
			map[string]any{
				"name": "mismatch", "request": map[string]any{"filename": "a.py", "config": map[string]any{"a": 1}},
				"expect_config": map[string]any{"a": 2},
			},
			map[string]any{
				"name": "decode-error", "request": map[string]any{"filename": "a.py"},
				"stdout": "not json", "expect_error": "decode tool response",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	command := Command{Stdin: bytes.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}}
	if status := command.Main([]string{"actionlint", "plugin", "test"}); status != 1 {
		t.Fatalf("status=%d stdout=%s", status, stdout.String())
	}
	var report struct {
		Valid bool `json:"valid"`
		Cases []struct {
			Passed bool   `json:"passed"`
			Error  string `json:"error"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Valid || len(report.Cases) != 2 || report.Cases[0].Passed || !strings.Contains(report.Cases[0].Error, "expect_config") {
		t.Fatalf("report=%s", stdout.String())
	}
	if !report.Cases[1].Passed || !strings.Contains(report.Cases[1].Error, "decode tool response") {
		t.Fatalf("expected pipeline error failed: %s", stdout.String())
	}
}

func TestPluginTestRejectsUnknownAndInvalidFixtureFields(t *testing.T) {
	t.Parallel()
	manifest, err := json.Marshal(authoringTestManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		fixture string
		want    string
	}{
		{`{"name":"unknown","request":{"filename":"a"},"expect_config":{},"surprise":true}`, "unknown field"},
		{`{"name":"enum","request":{"filename":"a"},"expect":{"diagnostics":[{"template_effect":"unknown"}]}}`, "template_effect has unknown value"},
	} {
		input, err := json.Marshal(map[string]json.RawMessage{
			"manifest": manifest,
			"cases":    json.RawMessage("[" + testCase.fixture + "]"),
		})
		if err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		command := Command{Stdin: bytes.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}}
		if status := command.Main([]string{"actionlint", "plugin", "test"}); status != 1 || !strings.Contains(stdout.String(), testCase.want) {
			t.Errorf("status=%d stdout=%s want=%q", status, stdout.String(), testCase.want)
		}
	}
}

func TestPluginTestAcceptsExpectationWithoutTemplateEffect(t *testing.T) {
	t.Parallel()
	manifest := authoringTestManifest(t)
	manifest.Capabilities.TemplateContract = ""
	manifest.Process.TemplateEffects = nil
	input, err := json.Marshal(map[string]any{
		"manifest": manifest,
		"cases": []any{map[string]any{
			"name":    "expected-finding",
			"request": map[string]any{"filename": "a.py", "source": "x"},
			"stdout":  `{"ok":true,"diagnostics":[{"filename":"a.py","code":"E1","message":"bad","severity":"warning","location":{"row":1,"column":1}}]}`,
			"expect": map[string]any{"diagnostics": []any{map[string]any{
				"filename": "a.py", "code": "E1", "message": "bad", "severity": "warning",
				"location": map[string]int{"row": 1, "column": 1},
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	command := Command{Stdin: bytes.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}}
	if status := command.Main([]string{"actionlint", "plugin", "test"}); status != 0 {
		t.Fatalf("status=%d stdout=%s", status, stdout.String())
	}
	var report struct {
		Valid bool `json:"valid"`
		Cases []struct {
			Passed bool `json:"passed"`
			Result struct {
				Diagnostics []json.RawMessage `json:"diagnostics"`
			} `json:"result"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Valid || len(report.Cases) != 1 || !report.Cases[0].Passed {
		t.Fatalf("report=%s", stdout.String())
	}
	if len(report.Cases[0].Result.Diagnostics) != 1 {
		t.Fatalf("diagnostics=%s", stdout.String())
	}
	var diagnostic map[string]json.RawMessage
	if err := json.Unmarshal(report.Cases[0].Result.Diagnostics[0], &diagnostic); err != nil {
		t.Fatal(err)
	}
	if _, present := diagnostic["end_location"]; present {
		t.Errorf("point finding includes end_location: %s", report.Cases[0].Result.Diagnostics[0])
	}
}

func authoringTestManifest(t *testing.T) pluginmanifests.Manifest {
	t.Helper()
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if manifest.Rule.Name != "ruff" {
			continue
		}
		manifest.Process.Entry = "actionlint-authoring-must-not-run"
		manifest.ConfigSchema = nil
		manifest.ConfigShorthands = nil
		manifest.Process.ConfigSchema = nil
		manifest.Process.Config.Transform = &pluginmanifests.Transform{
			Language: pluginmanifests.LanguageJQ,
			Inline:   `{config: (. + {defaulted:true}), vars:{added:"yes"}}`,
		}
		manifest.Process.Config.Forbidden = nil
		manifest.Process.Output = pluginmanifests.Output{Format: pluginmanifests.FormatEnvelope}
		return manifest
	}
	t.Fatal("bundled Ruff provider missing")
	return pluginmanifests.Manifest{}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}
