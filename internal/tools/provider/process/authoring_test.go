package process

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

func authoringManifest(t *testing.T, output string) provider.Manifest {
	t.Helper()
	manifest := bundledManifest(t, "ruff")
	manifest.Process.Entry = "actionlint-authoring-must-not-run"
	manifest.ConfigSchema = nil
	manifest.Process.ConfigSchema = nil
	manifest.Process.Config.Transform = &provider.Transform{
		Language: provider.LanguageJQ,
		Inline:   `{config: {selected: .selected}, vars: {mode: .mode, drop: null}}`,
	}
	manifest.Process.Config.Forbidden = nil
	manifest.Process.Output = provider.Output{
		Format:    provider.FormatJSON,
		Transform: &provider.Transform{Language: provider.LanguageJQ, Inline: output},
	}
	manifest.Capabilities.Fixes = true
	return manifest
}

func TestValidateManifestCompilesSandboxedProgramsWithoutRunningEntry(t *testing.T) {
	manifest := authoringManifest(t, `[]`)
	if err := ValidateManifest(manifest, nil); err != nil {
		t.Fatal(err)
	}
	for _, program := range []string{`[`, `import "x" as x; []`, `inputs`, `input`} {
		manifest.Process.Output.Transform.Inline = program
		if err := ValidateManifest(manifest, nil); err == nil {
			t.Errorf("accepted jq %q", program)
		}
	}
}

func TestEvaluateFixtureUsesRuntimeTransformsAndReviewsOutput(t *testing.T) {
	manifest := authoringManifest(t, `[{code:"E1", filename:$vars.filename, message:$vars.mode, severity:"warning", location:{row:1,column:1}, fixes:[{description:"fix",replacements:[{row:1,column:1,end_row:1,end_column:2,text:"z"}]}]}]`)
	config := json.RawMessage(`{"selected":true,"mode":"config"}`)
	request := analyzer.ScriptRequest{Source: "abc", Filename: "snippet.py", Config: config}
	stdout := `{}`
	got, err := EvaluateFixture(context.Background(), manifest, nil, request, &stdout, map[string]string{"mode": "fixture", "drop": "present"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Config) != `{"selected":true}` {
		t.Errorf("config=%s", got.Config)
	}
	if got.Variables["mode"] != "config" || got.Variables["drop"] != "" || got.Variables["filename"] != "snippet.py" {
		t.Errorf("variables=%v", got.Variables)
	}
	if got.Result == nil || len(got.Result.Diagnostics) != 1 {
		t.Fatalf("result=%+v", got.Result)
	}
	diagnostic := got.Result.Diagnostics[0]
	if diagnostic.Filename != "snippet.py" || diagnostic.Message != "config" || len(diagnostic.Fixes) != 1 || diagnostic.Fixes[0].Replacements[0].Text != "z" {
		t.Errorf("diagnostic=%+v", diagnostic)
	}
}

func TestEvaluateFixtureFixApplicability(t *testing.T) {
	for _, label := range []string{"safe", "unsafe", "suggested", "automatic"} {
		manifest := authoringManifest(t, `[{code:"E1", filename:$vars.filename, message:"fix", location:{row:1,column:1}, fixes:[{applicability:"`+label+`",replacements:[{row:1,column:1,end_row:1,end_column:2,text:"z"}]}]}]`)
		stdout := `{}`
		got, err := EvaluateFixture(t.Context(), manifest, nil, analyzer.ScriptRequest{Source: "abc", Filename: "snippet.py"}, &stdout, nil, nil)
		want, parseErr := diagnostic.ParseApplicability(label)
		if parseErr != nil {
			if err == nil {
				t.Fatalf("accepted unknown applicability %q", label)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got.Result == nil || len(got.Result.Diagnostics) != 1 || len(got.Result.Diagnostics[0].Fixes) != 1 || got.Result.Diagnostics[0].Fixes[0].Applicability != want {
			t.Fatalf("lost %s applicability in the process protocol: %+v", label, got.Result)
		}
	}
}

func TestEvaluateFixtureChecksOutputAndConfigFailures(t *testing.T) {
	manifest := authoringManifest(t, `[{code:"E1", filename:"other.py", message:"bad", severity:"warning", location:{row:1,column:1}}]`)
	stdout := `{}`
	_, err := EvaluateFixture(context.Background(), manifest, nil, analyzer.ScriptRequest{Source: "abc", Filename: "snippet.py"}, &stdout, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("error=%v, want source identity validation", err)
	}
	manifest.Process.Config.Forbidden = []string{"selected"}
	_, err = EvaluateFixture(context.Background(), manifest, nil, analyzer.ScriptRequest{Filename: "snippet.py", Config: json.RawMessage(`{"selected":true}`)}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("error=%v, want forbidden config error", err)
	}
}

func TestEvaluateFixtureJQHasEmptyEnvironment(t *testing.T) {
	manifest := authoringManifest(t, `[]`)
	manifest.Process.Config.Transform.Inline = `{config:{variable:$ENV, function:env},vars:{}}`
	got, err := EvaluateFixture(context.Background(), manifest, nil, analyzer.ScriptRequest{Filename: "a.py"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Config) != `{"function":{},"variable":{}}` {
		t.Errorf("config=%s", got.Config)
	}
}

func TestAuthoringTesterHonorsCancellation(t *testing.T) {
	manifest := authoringManifest(t, `[]`)
	tester, err := NewAuthoringTester(manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tester.Evaluate(canceled, analyzer.ScriptRequest{Filename: "a.py"}, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled evaluation error=%v", err)
	}

	manifest.Process.Config.Transform.Inline = `{config:(def loop: loop; loop),vars:{}}`
	tester, err = NewAuthoringTester(manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	timed, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = tester.Evaluate(timed, analyzer.ScriptRequest{Filename: "a.py"}, nil, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("nonterminating jq error=%v, want deadline", err)
	}
}
