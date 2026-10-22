package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/analyzer"
	"github.com/partite-ai/wacogo"
)

func TestScriptDescriptorShellValidation(t *testing.T) {
	p := loadScriptFixture(t)
	results, err := p.callInterface(t.Context(), ScriptEngineInterface, "describe")
	if err != nil {
		t.Fatal(err)
	}
	original, err := liftRecord(results[0], "script descriptor")
	if err != nil {
		t.Fatal(err)
	}
	large := make([]wacogo.Val, 10000)
	for i := range large {
		large[i] = wacogo.ValString(fmt.Sprintf("shell-%d", i))
	}
	for _, tc := range []struct {
		name   string
		shells []wacogo.Val
		valid  bool
	}{
		{"large catalogue", large, true},
		{"canonical names", []wacogo.Val{wacogo.ValString("python"), wacogo.ValString("bash"), wacogo.ValString("python3.14")}, true},
		{"uppercase", []wacogo.Val{wacogo.ValString("Python")}, false},
		{"case aliases", []wacogo.Val{wacogo.ValString("python"), wacogo.ValString("Python")}, false},
		{"executable suffix", []wacogo.Val{wacogo.ValString("python.exe")}, false},
		{"path", []wacogo.Val{wacogo.ValString("/usr/bin/python")}, false},
		{"arguments", []wacogo.Val{wacogo.ValString("python -u")}, false},
		{"duplicate", []wacogo.Val{wacogo.ValString("python"), wacogo.ValString("python")}, false},
		{"empty name", []wacogo.Val{wacogo.ValString("")}, false},
		{"wrong type", []wacogo.Val{wacogo.ValBool(true)}, false},
		{"input-only catalogue validated by static metadata", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := wacogo.NewValRecord(field("id", original.Field("id")), field("version", original.Field("version")),
				field("name", original.Field("name")), field("engine", original.Field("engine")), field("engine-version", original.Field("engine-version")),
				field("shells", wacogo.NewValListOf(tc.shells...)), field("rule", original.Field("rule")))
			descriptor, err := liftScriptDescriptor(value)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t: %v", tc.valid, err)
			}
			if tc.valid {
				for i, shell := range descriptor.Shells {
					if wacogo.ValString(shell) != tc.shells[i] {
						t.Fatalf("shell order changed at %d", i)
					}
				}
			}
		})
	}
}

func TestScriptDescriptorRuleNameFitsInlineSuppressionSelector(t *testing.T) {
	p := loadScriptFixture(t)
	results, err := p.callInterface(t.Context(), ScriptEngineInterface, "describe")
	if err != nil {
		t.Fatal(err)
	}
	original, err := liftRecord(results[0], "script descriptor")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		valid bool
	}{
		{"no,sudo", false}, {"no -- x", false}, {"name --", false},
		{" no-sudo", false}, {"no-sudo ", false}, {"no\nsudo", false},
		{"no-sudo", true}, {"NO.SUDO", true}, {"org/foo", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := scriptDescriptorWithRuleName(original, test.name)
			if err != nil {
				t.Fatal(err)
			}
			_, err = liftScriptDescriptor(descriptor)
			if (err == nil) != test.valid {
				t.Fatalf("name=%q valid=%v: %v", test.name, test.valid, err)
			}
		})
	}
}

func scriptDescriptorWithRuleName(original *wacogo.ValRecord, name string) (wacogo.Val, error) {
	rule, err := liftRecord(original.Field("rule"), "script rule descriptor")
	if err != nil {
		return nil, err
	}
	ruleValue := wacogo.NewValRecord(
		field("name", wacogo.ValString(name)),
		field("description", rule.Field("description")),
		field("category", rule.Field("category")),
		field("subcategory", rule.Field("subcategory")),
		field("maturity", rule.Field("maturity")),
		field("provider", rule.Field("provider")),
		field("explanation", rule.Field("explanation")),
		field("help", rule.Field("help")),
		field("documentation-url", rule.Field("documentation-url")),
		field("recommended", rule.Field("recommended")),
		field("options-schema", rule.Field("options-schema")),
		field("examples", rule.Field("examples")),
	)
	return wacogo.NewValRecord(
		field("id", original.Field("id")),
		field("version", original.Field("version")),
		field("name", original.Field("name")),
		field("engine", wacogo.ValString(name)),
		field("engine-version", original.Field("engine-version")),
		field("shells", original.Field("shells")),
		field("rule", ruleValue),
	), nil
}

func loadScriptFixture(t *testing.T) *Plugin {
	t.Helper()
	p, err := Load(t.Context(), "testdata/plugins/script-engine.wasm", Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return p
}

func TestScriptEngineComponent(t *testing.T) {
	p := loadScriptFixture(t)
	if !p.HasScriptEngine() || p.HasAnalyzer() || p.HasRunnerProvider() {
		t.Fatal("script-only component advertised wrong roles")
	}
	descriptor := p.ScriptDescriptor()
	if descriptor.ID != "echo-component" || descriptor.Engine != "echo" || descriptor.Rule.Provider != descriptor.ID {
		t.Fatalf("descriptor: %#v", descriptor)
	}
	descriptor.Shells[0] = "changed"
	descriptor.Rule.OptionsSchema[0] = '!'
	if got := p.ScriptDescriptor(); got.Shells[0] != "python" || !json.Valid(got.Rule.OptionsSchema) {
		t.Fatal("descriptor aliases caller memory")
	}
	for _, config := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		if err := p.ValidateConfig(t.Context(), config); err != nil {
			t.Fatal(err)
		}
	}
	for _, config := range []string{`null`, `[]`, `{`, `{"invalid":true}`} {
		if err := p.ValidateConfig(t.Context(), json.RawMessage(config)); err == nil {
			t.Fatalf("accepted invalid options %s", config)
		}
	}
	request := analyzer.ScriptRequest{Source: "é = missing\r\nmissing\n", Filename: "script.py"}
	for range 2 {
		found, err := p.Check(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 2 || found[0].Location.Column != 5 || found[0].EndLocation.Column != 12 ||
			found[1].Location.Row != 2 || found[0].Severity != "warning" || found[0].URL != "https://example.com/echo" {
			t.Fatalf("findings: %#v", found)
		}
	}
	request.Config = json.RawMessage(`{"mode":"partial"}`)
	found, err := p.Check(t.Context(), request)
	if len(found) != 2 || err == nil || !strings.Contains(err.Error(), "fixture analysis incomplete") {
		t.Fatalf("partial outcome: %#v, %v", found, err)
	}
	request.Config = json.RawMessage(`{"mode":"invalid"}`)
	if found, err := p.Check(t.Context(), request); err == nil || len(found) != 0 {
		t.Fatalf("invalid second finding leaked first: %#v, %v", found, err)
	}
}

func TestScriptEngineResultLimits(t *testing.T) {
	p := loadScriptFixture(t)
	for _, request := range []analyzer.ScriptRequest{
		{Source: strings.Repeat("missing\n", maxScriptDiagnostics+1), Filename: "script.py"},
		{Source: "missing", Filename: "script.py", Config: json.RawMessage(`{"mode":"large-message"}`)},
	} {
		if found, err := p.Check(t.Context(), request); err == nil || len(found) != 0 {
			t.Fatalf("oversized result accepted: %d findings, %v", len(found), err)
		}
	}
}

func TestScriptEngineUniversalNewlines(t *testing.T) {
	p := loadScriptFixture(t)
	for _, tc := range []struct {
		source string
		starts []analyzer.ScriptPosition
	}{
		{"é = missing\rmissing", []analyzer.ScriptPosition{{Row: 1, Column: 5}, {Row: 1, Column: 13}}},
		{"é = missing\r\nmissing", []analyzer.ScriptPosition{{Row: 1, Column: 5}, {Row: 2, Column: 1}}},
		{"é = missing\nmissing", []analyzer.ScriptPosition{{Row: 1, Column: 5}, {Row: 2, Column: 1}}},
		{"é = missing\r\nmissing\rmissing\nmissing\r", []analyzer.ScriptPosition{{Row: 1, Column: 5}, {Row: 2, Column: 1}, {Row: 2, Column: 9}, {Row: 3, Column: 1}}},
	} {
		t.Run(fmt.Sprintf("%q", tc.source), func(t *testing.T) {
			found, err := p.Check(t.Context(), analyzer.ScriptRequest{Source: tc.source, Filename: "script.py"})
			if err != nil {
				t.Fatal(err)
			}
			if len(found) != len(tc.starts) {
				t.Fatalf("findings: %#v", found)
			}
			for i, finding := range found {
				start := tc.starts[i]
				if finding.Location != start ||
					finding.EndLocation != (analyzer.ScriptPosition{Row: start.Row, Column: start.Column + 7}) {
					t.Fatalf("finding %d: %#v", i, finding)
				}
			}
		})
	}
}

func TestScriptLineWidths(t *testing.T) {
	for _, tc := range []struct {
		source string
		widths []int
	}{
		{"", []int{0}},
		{"é\r\n界\ra\n", []int{1, 1, 1, 0}},
		{"\r\r\n\n", []int{0, 0, 0, 0}},
		{"a\u2028b", []int{3}},
	} {
		if got := scriptLineWidths(tc.source); !slices.Equal(got, tc.widths) {
			t.Fatalf("%q: widths %v, want %v", tc.source, got, tc.widths)
		}
	}
}

func TestScriptEngineCancellationAndRecovery(t *testing.T) {
	p := loadScriptFixture(t)
	request := analyzer.ScriptRequest{Source: "missing", Filename: "script.py"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Check(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled check: %v", err)
	}
	// Queue admission is also cancellable, without disturbing another caller.
	if err := p.gate.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	_, err := p.Check(ctx, request)
	cancel()
	p.gate.Release(1)
	if err == nil || !strings.Contains(err.Error(), "exceeded its time limit") {
		t.Fatalf("queued check: %v", err)
	}
	for _, source := range []string{"trap", "loop"} {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		_, err := p.Check(ctx, analyzer.ScriptRequest{Source: source, Filename: "script.py"})
		cancel()
		if err == nil {
			t.Fatalf("%s succeeded", source)
		}
		if found, err := p.Check(t.Context(), request); err != nil || len(found) != 1 {
			t.Fatalf("recovery after %s: %#v, %v", source, found, err)
		}
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Check(t.Context(), request); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed check: %v", err)
	}
	if err := p.ValidateConfig(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed validation: %v", err)
	}
}

func TestScriptEngineContractCurrent(t *testing.T) {
	data, err := os.ReadFile("../../../plugins/wit/script-engine.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != scriptEngineContractDigest {
		t.Fatal("stale script engine contract; regenerate")
	}
}

func TestScriptEngineRejectsOtherRoles(t *testing.T) {
	p := loadRunnerFixture(t)
	if _, err := p.Check(t.Context(), analyzer.ScriptRequest{Source: "missing", Filename: "script.py"}); err == nil {
		t.Fatal("provider accepted script request")
	}
	if err := p.ValidateConfig(t.Context(), nil); err == nil {
		t.Fatal("provider accepted script options")
	}
}

func TestScriptEngineStaticMetadataAgreement(t *testing.T) {
	p := loadScriptFixture(t)
	d := p.ScriptDescriptor()
	metadata := map[string]any{
		"metadata_version": 1, "id": d.ID, "name": d.Name, "plugin_version": d.Version,
		"transport": "wasm-component", "interface": ScriptEngineInterface, "contract_sha256": scriptEngineContractDigest,
		"engine":       map[string]string{"name": d.Engine, "version": d.EngineVersion},
		"capabilities": map[string]any{"script_shells": d.Shells, "fixes": false, "format": false},
		"rules":        []any{d.Rule}, "config_schema": d.Rule.OptionsSchema,
	}
	for _, key := range []string{"", "id", "plugin_version", "engine", "capabilities", "rules", "config_schema", "contract_sha256"} {
		t.Run(key, func(t *testing.T) {
			original := metadata[key]
			if key != "" {
				metadata[key] = nil
				defer func() { metadata[key] = original }()
			}
			data, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			err = p.verifyScriptMetadata(data)
			if (err == nil) != (key == "") {
				t.Fatalf("metadata %q: %v", key, err)
			}
		})
	}
	for _, shells := range [][]string{{"python"}, {"Python"}, {"python.exe"}, {"python", "Python"}} {
		t.Run("shells="+strings.Join(shells, ","), func(t *testing.T) {
			metadata["capabilities"] = map[string][]string{"script_shells": shells}
			data, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.verifyScriptMetadata(data); (err == nil) != slices.Equal(shells, d.Shells) {
				t.Fatalf("metadata shells=%v: %v", shells, err)
			}
		})
	}
}

func TestScriptEngineRejectsChangedInterface(t *testing.T) {
	data, err := os.ReadFile("testdata/plugins/script-engine.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, replacement string }{
		{ScriptEngineInterface, "kjanat:actionlint/script-engine@0.2.0"},
		{"end-location", "bad-location"},
	} {
		t.Run(change.old, func(t *testing.T) {
			changed := bytes.ReplaceAll(data, []byte(change.old), []byte(change.replacement))
			if bytes.Equal(changed, data) {
				t.Fatal("fixture did not contain interface field")
			}
			path := filepath.Join(t.TempDir(), "changed.wasm")
			if err := os.WriteFile(path, changed, 0600); err != nil {
				t.Fatal(err)
			}
			p, err := Load(t.Context(), path, Options{})
			if p != nil {
				_ = p.Close(t.Context())
			}
			if err == nil {
				t.Fatal("changed interface accepted")
			}
		})
	}
}
