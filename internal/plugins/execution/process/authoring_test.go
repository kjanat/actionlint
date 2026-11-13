package process

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/bundled"
	"actionlint.kjanat.dev/v2/internal/results"
)

func authoringManifest(t *testing.T, output string) manifests.Manifest {
	t.Helper()
	manifest := bundledManifest(t, "ruff")
	manifest.Process.Entry = "actionlint-authoring-must-not-run"
	manifest.ConfigSchema = nil
	manifest.Process.ConfigSchema = nil
	manifest.Process.Config.Transform = &manifests.Transform{
		Language: manifests.LanguageJQ,
		Inline:   `{config: {selected: .selected}, vars: {mode: .mode, drop: null}}`,
	}
	manifest.Process.Config.Forbidden = nil
	manifest.Process.Output = manifests.Output{
		Format:    manifests.FormatJSON,
		Transform: &manifests.Transform{Language: manifests.LanguageJQ, Inline: output},
	}
	manifest.Capabilities.Fixes = true
	return manifest
}

func TestValidateManifestCompilesSandboxedProgramsWithoutRunningEntry(t *testing.T) {
	t.Parallel()
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

func TestPathExpansionValidatesUserAndExecutionSettingsSeparately(t *testing.T) {
	t.Parallel()
	manifest := authoringManifest(t, `[]`)
	manifest.ConfigurationPaths = []string{"/path"}
	manifest.ConfigSchema = json.RawMessage(`{"type":"object","properties":{"path":{"const":"relative"}},"additionalProperties":false}`)
	manifest.Process.ConfigSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","pattern":"^/"}},"additionalProperties":false}`)
	manifest.Process.Config.Transform = nil
	backend, err := authoringBackend(manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ValidateConfig(t.Context(), []byte(`{"path":"/repo/relative"}`)); err != nil {
		t.Fatalf("expanded path was checked against the user schema: %v", err)
	}
	if err := backend.ValidateConfig(t.Context(), []byte(`{"path":"relative"}`)); err == nil {
		t.Fatal("execution schema was skipped")
	}
	stdout := `[]`
	if _, err := EvaluateFixture(t.Context(), manifest, nil, checkinputs.ScriptRequest{Source: "pass", Filename: "snippet.py", Config: []byte(`{"path":"wrong"}`)}, &stdout, nil, nil); err == nil {
		t.Fatal("user schema was skipped for path-only configuration")
	}
}

func TestEvaluateFixtureUsesRuntimeTransformsAndReviewsOutput(t *testing.T) {
	t.Parallel()
	manifest := authoringManifest(t, `[{code:"E1", filename:$vars.filename, message:$vars.mode, severity:"warning", location:{row:1,column:1}, fixes:[{description:"fix",replacements:[{row:1,column:1,end_row:1,end_column:2,text:"z"}]}]}]`)
	config := json.RawMessage(`{"selected":true,"mode":"config"}`)
	request := checkinputs.ScriptRequest{Source: "abc", Filename: "snippet.py", Config: config}
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
	t.Parallel()
	for _, label := range []string{"safe", "unsafe", "suggested", "automatic"} {
		manifest := authoringManifest(t, `[{code:"E1", filename:$vars.filename, message:"fix", location:{row:1,column:1}, fixes:[{applicability:"`+label+`",replacements:[{row:1,column:1,end_row:1,end_column:2,text:"z"}]}]}]`)
		stdout := `{}`
		got, err := EvaluateFixture(t.Context(), manifest, nil, checkinputs.ScriptRequest{Source: "abc", Filename: "snippet.py"}, &stdout, nil, nil)
		want, parseErr := results.ParseApplicability(label)
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
	t.Parallel()
	manifest := authoringManifest(t, `[{code:"E1", filename:"other.py", message:"bad", severity:"warning", location:{row:1,column:1}}]`)
	stdout := `{}`
	_, err := EvaluateFixture(context.Background(), manifest, nil, checkinputs.ScriptRequest{Source: "abc", Filename: "snippet.py"}, &stdout, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("error=%v, want source identity validation", err)
	}
	manifest.Process.Config.Forbidden = []string{"selected"}
	_, err = EvaluateFixture(context.Background(), manifest, nil, checkinputs.ScriptRequest{Filename: "snippet.py", Config: json.RawMessage(`{"selected":true}`)}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("error=%v, want forbidden config error", err)
	}
}

func TestEvaluateFixtureJQHasEmptyEnvironment(t *testing.T) {
	t.Parallel()
	manifest := authoringManifest(t, `[]`)
	manifest.Process.Config.Transform.Inline = `{config:{variable:$ENV, function:env},vars:{}}`
	got, err := EvaluateFixture(context.Background(), manifest, nil, checkinputs.ScriptRequest{Filename: "a.py"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Config) != `{"function":{},"variable":{}}` {
		t.Errorf("config=%s", got.Config)
	}
}

func TestAuthoringTesterHonorsCancellation(t *testing.T) {
	t.Parallel()
	manifest := authoringManifest(t, `[]`)
	tester, err := NewAuthoringTester(manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tester.Evaluate(canceled, checkinputs.ScriptRequest{Filename: "a.py"}, nil, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled evaluation error=%v", err)
	}

	manifest.Process.Config.Transform.Inline = `{config:(def loop: loop; loop),vars:{}}`
	tester, err = NewAuthoringTester(manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	timed, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = tester.Evaluate(timed, checkinputs.ScriptRequest{Filename: "a.py"}, nil, nil, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("nonterminating jq error=%v, want deadline", err)
	}
}

func TestAuthoringEvaluatesShellCheckPreparation(t *testing.T) {
	t.Parallel()
	manifest := bundledManifest(t, "shellcheck")
	tester, err := NewAuthoringTester(manifest, bundled.Program)
	if err != nil {
		t.Fatal(err)
	}
	facts := preparation.ScriptContext{Language: "bash", Startup: "set -e", EnvironmentStartup: "export lowercase", EnvironmentKnown: true}
	request := checkinputs.ScriptRequest{Filename: "script.sh", Source: "# shellcheck shell=sh\n[[ -n \"$HOME\" ]]\n", Config: json.RawMessage(`{"disable":["SC2086"]}`)}
	stdout := `{"comments":[]}`
	result, err := tester.Evaluate(t.Context(), request, &stdout, nil, nil, &facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Preparation == nil || result.Variables["dialect"] != "" || strings.Contains(result.Source, "set -e") || !strings.Contains(result.Source, "export lowercase") || !strings.Contains(result.Source, "# shellcheck disable=SC2086") {
		t.Fatalf("result=%+v", result)
	}
	if _, present := result.Variables["dialect"]; present {
		t.Fatal("explicit directive must reach ShellCheck without a CLI shell override")
	}
	if len(result.Preparation.Excluded) != 5 || strings.Contains(result.Variables["exclude"], "SC2154") {
		t.Fatalf("known environment exclusions=%v", result.Preparation.Excluded)
	}
	facts.EnvironmentKnown = false
	result, err = tester.Evaluate(t.Context(), request, nil, nil, nil, &facts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Variables["exclude"], "SC2154") {
		t.Fatal("unknown environment did not disable unassigned checks")
	}
	facts.Language = ""
	request.Source = "print(1)\n"
	result, err = tester.Evaluate(t.Context(), request, &stdout, nil, nil, &facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Preparation.Skip == "" || result.Result != nil {
		t.Fatalf("unsupported source was processed: %+v", result)
	}
}
