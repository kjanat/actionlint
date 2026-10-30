package process

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
)

func TestTOMLPairs(t *testing.T) {
	for _, tc := range []struct {
		config string
		want   []string
	}{
		{``, nil},
		{`null`, nil},
		{`{}`, nil},
		{`{"line-length": 100, "skip": null}`, []string{"--config", `"line-length" = 100`}},
		{`{"lint": {"select": ["F", "E"], "ignore": null, "per-file-ignores": {"a=b.py": ["F401"]}}, "preview": true}`, []string{
			"--config", `"lint" = {"per-file-ignores" = {"a=b.py" = ["F401"]}, "select" = ["F", "E"]}`,
			"--config", `"preview" = true`,
		}},
		{`{"big": 123456789012345678901234567890, "float": 1.50}`, []string{"--config", `"big" = 123456789012345678901234567890`, "--config", `"float" = 1.50`}},
		{`{"quote": "say \"hi\"\n"}`, []string{"--config", `"quote" = "say \"hi\"\n"`}},
		{`{"--fix": "--fix"}`, []string{"--config", `"--fix" = "--fix"`}},
	} {
		got, err := TOMLPairs("--config", json.RawMessage(tc.config))
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, %v; want %q", tc.config, got, err, tc.want)
		}
	}
	for _, config := range []string{`[1]`, `"x"`, `{"list": [1, null]}`} {
		if _, err := TOMLPairs("--config", json.RawMessage(config)); err == nil {
			t.Errorf("rendered %s", config)
		}
	}
}

func TestRuffConfigTransform(t *testing.T) {
	manifest := bundledManifest(t, "ruff")
	source, err := program(manifest.Process.Config.Transform, bundled.Program)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileJQ(source)
	if err != nil {
		t.Fatal(err)
	}
	transform := configTransform{program: compiled}
	py312 := "py312"
	for _, tc := range []struct {
		config, want string
		vars         map[string]*string
	}{
		{``, `{}`, nil},
		{`{"lint": {"select": []}}`, `{"lint": {"select": ["ALL"], "ignore": ["ALL"]}}`, nil},
		{`{"lint": {"select": ["F"]}, "line-length": 100}`, `{"lint": {"select": ["F"]}, "line-length": 100}`, nil},
		{`{"target-version": "py312"}`, `{"target-version": "py312"}`, map[string]*string{"target_version": nil}},
	} {
		config, err := decodeConfig([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		got, err := transform.apply(t.Context(), config, map[string]string{"target_version": py312})
		if err != nil {
			t.Fatalf("%s: %v", tc.config, err)
		}
		want, err := decodeConfig([]byte(tc.want))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got.value); diff != "" {
			t.Errorf("%s: config (-want +got):\n%s", tc.config, diff)
		}
		if diff := cmp.Diff(tc.vars, got.vars); diff != "" && (len(tc.vars) != 0 || len(got.vars) != 0) {
			t.Errorf("%s: vars (-want +got):\n%s", tc.config, diff)
		}
	}
}

func TestConfigTransformContract(t *testing.T) {
	for _, tc := range []struct{ program, problem string }{
		{`[.]`, "must produce an object"},
		{`{config: ., extra: 1}`, "unknown member"},
		{`{vars: {}}`, `missing "config"`},
		{`{config: ., vars: {a: 1}}`, "string or null"},
		{`{config: ., vars: []}`, "vars must be an object"},
		{`$ENV.HOME`, "must produce an object"},
	} {
		compiled, err := compileJQ(tc.program)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&configTransform{program: compiled}).apply(t.Context(), map[string]any{}, nil)
		if err == nil || !strings.Contains(err.Error(), tc.problem) {
			t.Errorf("%s: %v, want %q", tc.program, err, tc.problem)
		}
	}
}

func TestRuffForbiddenConfig(t *testing.T) {
	forbidden := bundledManifest(t, "ruff").Process.Config.Forbidden
	if len(forbidden) == 0 {
		t.Fatal("bundled Ruff manifest forbids no configuration keys")
	}
	for _, key := range forbidden {
		if err := checkForbidden("ruff", map[string]any{key: nil}, forbidden); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("accepted %s: %v", key, err)
		}
	}
	if err := checkForbidden("tool", map[string]any{"lint": map[string]any{"fix": true}}, []string{"lint.fix"}); err == nil {
		t.Error("accepted a forbidden nested key")
	}
	if err := checkForbidden("tool", map[string]any{"lint": map[string]any{"select": []any{"F"}}}, []string{"lint.fix", "fix"}); err != nil {
		t.Error(err)
	}
}

func TestConfigFiles(t *testing.T) {
	config, err := decodeConfig([]byte(`{"line-length": 100, "lint": {"select": ["F"], "unset": null}, "ratio": 0.5}`))
	if err != nil {
		t.Fatal(err)
	}
	for render, want := range map[provider.Render]string{
		provider.RenderTOMLFile: "line-length = 100\nratio = 0.5\n\n[lint]\nselect = ['F']\n",
		provider.RenderJSONFile: `{"line-length":100,"lint":{"select":["F"],"unset":null},"ratio":0.5}`,
	} {
		path, remove, err := writeConfigFile(render, config)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		remove()
		if err != nil || string(data) != want {
			t.Errorf("%s: got %q, %v; want %q", render, data, err, want)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: temporary file survived: %v", render, err)
		}
	}
}
