package validation_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestActionMetadataLiteralKeys(t *testing.T) {
	const source = `name: test
description: test
runs:
  using: composite
  steps:
    - run: echo ok
      shell: bash
`
	for _, key := range []string{"name", "description", "runs", "using", "steps", "run", "shell"} {
		for _, spelling := range []string{key, strings.ToUpper(key)} {
			t.Run(spelling, func(t *testing.T) {
				input := strings.Replace(source, key+":", "${{ '"+spelling+"' }}:", 1)
				meta, err := actionmeta.Parse("action.yml", []byte(input))
				if err != nil {
					t.Fatal(err)
				}
				if meta.Runs.Using != "composite" || len(meta.Runs.Steps) != 1 || meta.Runs.Steps[0].Run == nil {
					t.Fatalf("execution fields were lost: %#v", meta.Runs)
				}
				meta.Directory, meta.Filename = t.TempDir(), "action.yml"
				findings := actioncheck.Check(meta, &workflowlang.Pos{Line: 1, Col: 1})
				if errs := findings; len(errs) != 0 {
					t.Fatalf("literal field key rejected: %v", errs)
				}
			})
		}
	}
}

func TestActionMetadataLiteralKeysPreserveNamesAndSource(t *testing.T) {
	const source = `name: test
description: test
${{ 'Inputs' }}:
  ${{ 'MixedInput' }}: &definition
    ${{ 'Required' }}: true
    ${{ 'Default' }}: fallback
    ${{ 'DeprecationMessage' }}: Use another input
  SecondInput: *definition
${{ 'Outputs' }}:
  ${{ 'MixedOutput' }}: {}
${{ 'Branding' }}:
  ${{ 'Icon' }}: check
  ${{ 'Color' }}: blue
runs:
  ${{ 'Using' }}: composite
  steps:
    - ${{ 'Uses' }}: actions/checkout@v6
      ${{ 'With' }}:
        ${{ 'MixedInput' }}: value
      ${{ 'Env' }}:
        ${{ 'MixedEnv' }}: value
`
	var document syntax.Node
	if err := codec.Unmarshal([]byte(source), &document); err != nil {
		t.Fatal(err)
	}
	root := document.Content[0]
	var meta actionmeta.Metadata
	if err := actionmeta.Decode(root, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Root != root || root.Content[4].Value != "${{ 'Inputs' }}" {
		t.Fatal("source tree changed during metadata decoding")
	}
	for _, name := range []string{"MixedInput", "SecondInput"} {
		input := meta.Inputs[strings.ToLower(name)]
		if input == nil || input.Name != name || input.Required || !input.Deprecated || input.DeprecationMessage != "Use another input" {
			t.Fatalf("incorrect input %s: %#v", name, input)
		}
	}
	if output := meta.Outputs["mixedoutput"]; output == nil || output.Name != "MixedOutput" {
		t.Fatalf("output name changed: %#v", output)
	}
	if len(meta.InputDefaults) != 2 {
		t.Fatalf("expected two input defaults, got %d", len(meta.InputDefaults))
	}
	for i, name := range []string{"MixedInput", "SecondInput"} {
		if got := meta.InputDefaults[i]; got.Name != name || got.Value.Line != 6 {
			t.Fatalf("input default %s name or position changed: %#v", name, *got)
		}
	}
	if meta.Branding.Icon != "check" || meta.Branding.Color != "blue" {
		t.Fatalf("branding fields lost: %#v", meta.Branding)
	}
	if len(meta.Runs.Steps) != 1 {
		t.Fatalf("steps lost: %#v", meta.Runs)
	}
	step := meta.Runs.Steps[0]
	if step.Uses == nil || *step.Uses != "actions/checkout@v6" || len(step.With) != 1 || step.With[0].Name != "MixedInput" || len(step.Env) != 1 || step.Env[0].Name != "MixedEnv" {
		t.Fatalf("user-defined keys changed: %#v", step)
	}
}

func TestActionMetadataLiteralRunKeyStillChecksExpressions(t *testing.T) {
	const source = `name: test
description: test
runs:
  using: composite
  steps:
    - ${{ 'run' }}: echo ${{ secrets.TOKEN }}
      shell: bash
`
	var meta actionmeta.Metadata
	parsed, err := actionmeta.Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	meta = *parsed
	meta.Directory, meta.Filename = t.TempDir(), "action.yml"
	findings := actioncheck.Check(&meta, &workflowlang.Pos{Line: 1, Col: 1})
	errs := findings
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `context "secrets"`) {
		t.Fatalf("expected the original expression diagnostic, got %v", errs)
	}
}
