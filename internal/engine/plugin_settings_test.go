package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	diagmodel "actionlint.kjanat.dev/diagnostic"
	repoconfig "actionlint.kjanat.dev/internal/config"
)

type settingsAnalyzer struct {
	calls   int
	options json.RawMessage
}

func (*settingsAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "custom-check", Category: "security", Maturity: "stable", OptionsSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1}},"additionalProperties":false}`)}
}
func (a *settingsAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagmodel.Diagnostic, error) {
	a.calls++
	a.options = input.Options
	return []diagmodel.Diagnostic{{Message: "custom finding", Start: diagmodel.Position{Line: 1, Column: 1}}}, nil
}

func TestRegisteredAnalyzerSettings(t *testing.T) {
	const workflow = "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    steps:\n      - run: echo ok\n"
	for _, test := range []struct {
		name, configuration, severity, options string
		calls                                  int
	}{
		{"explicit registration", "{}", "", "", 1},
		{"severity and options", "lint: {rules: {plugins: {custom-check: {level: warn, options: {count: 2}}}}}", "warning", `{"count":2}`, 1},
		{"disabled rule", "lint: {rules: {plugins: {custom-check: off}}}", "", "", 0},
		{"disabled name", "lint: {rules: {disable: [custom-check]}}", "", "", 0},
		{"disabled group", "lint: {rules: {plugins: off}}", "", "", 0},
		{"disabled lint", "lint: {enabled: false}", "", "", 0},
		{"none preset", "lint: {rules: {preset: none}}", "", "", 0},
		{"recommended preset", "lint: {rules: {preset: recommended}}", "", "", 0},
		{"all preset", "lint: {rules: {preset: all}}", "", "", 1},
		{"override", "overrides:\n  - includes: ['example.yml']\n    lint: {rules: {plugins: {custom-check: info}}}", "info", "", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte(test.configuration), "custom-check")
			if err != nil {
				t.Fatal(err)
			}
			a := &settingsAnalyzer{}
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "example.yml", Content: []byte(workflow), Config: cfg}}, Analyzers: []analyzer.Registration{{Analyzer: a}}})
			if err != nil {
				t.Fatal(err)
			}
			if a.calls != test.calls || string(a.options) != test.options {
				t.Fatalf("calls=%d options=%s", a.calls, a.options)
			}
			if len(result.Diagnostics) != test.calls {
				t.Fatalf("findings: %+v", result.Diagnostics)
			}
			if test.calls != 0 && result.Diagnostics[0].Severity != test.severity {
				t.Fatalf("severity=%q want %q", result.Diagnostics[0].Severity, test.severity)
			}
		})
	}
	cfg, err := repoconfig.Parse([]byte("lint: {rules: {plugins: {custom-check: {level: on, options: {count: 0}}}}}"), "custom-check")
	if err != nil {
		t.Fatal(err)
	}
	a := &settingsAnalyzer{}
	request := AnalysisRequest{Sources: []SourceUnit{{Path: "example.yml", Content: []byte(workflow), Config: cfg}}, Analyzers: []analyzer.Registration{{Analyzer: a}}}
	if _, err := Analyze(t.Context(), request); err == nil || !strings.Contains(err.Error(), "options") || a.calls != 0 {
		t.Fatalf("invalid options were executed: calls=%d err=%v", a.calls, err)
	}
	request.Analyzers[0].Options = json.RawMessage(`{"count":3}`)
	if _, err := Analyze(t.Context(), request); err != nil || a.calls != 1 || string(a.options) != `{"count":3}` {
		t.Fatalf("explicit options lost precedence: calls=%d options=%s err=%v", a.calls, a.options, err)
	}
}

func TestRegisteredNurseryPresetSelection(t *testing.T) {
	descriptor := repoconfig.Rule{Name: "experimental-plugin", Maturity: "nursery", Group: "plugins"}
	for _, test := range []struct {
		root, group repoconfig.Preset
		want        repoconfig.Level
	}{
		{"", "", "on"}, {"all", "", "off"}, {"", "all", "off"}, {"recommended", "", "off"}, {"none", "", "off"},
	} {
		cfg := &repoconfig.Config{}
		cfg.Lint.Rules.Preset = test.root
		cfg.Lint.Rules.Plugins.Preset = test.group
		if got := repoconfig.SettingFor(cfg, descriptor, repoconfig.Presets{}, true).Level; got != test.want {
			t.Errorf("root=%s group=%s: %s want %s", test.root, test.group, got, test.want)
		}
	}
}
