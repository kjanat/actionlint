package actions_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func TestCompositeConditionalCheckoutKeepsUnaffectedPaths(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, tc := range []struct {
		name, destination, extra string
		read                     bool
	}{
		{"sibling", "mirror", "", true},
		{"same self checkout", "source", "", true},
		{"ancestor", ".", "", false},
		{"child", "source/local", "", false},
		{"unknown", "${{ inputs.path }}", "", false},
		{"tolerated sibling", "mirror", "\n  continue-on-error: true", true},
		{"tolerated same path", "source", "\n  continue-on-error: true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.WriteFixture(t, root, "refresh/action.yml", `name: refresh
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      with: {path: '`+tc.destination+"'}\n")
			steps := `- uses: actions/checkout@v6
  with: {path: source}
- uses: ./source/refresh
  if: inputs.refresh` + tc.extra + "\n- uses: ./source/local"
			result := testutil.Composite(t, root, steps, application.Options{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "action" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, "shell")
			})
			if found != tc.read {
				t.Fatalf("composite checked=%v, want %v: %+v", found, tc.read, result.Diagnostics)
			}
		})
	}
}

func TestRunDirectoryCaseInsensitiveSuffix(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFixture(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	want, err := filepath.EvalSymlinks(filepath.Join(root, "scripts", "Nested"))
	if err != nil {
		t.Fatal(err)
	}
	for _, checkout := range []string{"", "source"} {
		paths := execution.Paths{Workspace: root, Analysis: root, Reader: fsread.New(t.Context(), nil, nil), Placements: &execution.CheckoutPlacement{
			Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: checkout}, CaseInsensitive: true,
		}}
		mapped, known := paths.AnalysisPath(execution.JoinPath(checkout, "SCRIPTS/nested"))
		if !known || mapped != filepath.Join(root, "scripts", "Nested") {
			t.Fatalf("checkout %q: mapped=%q, known=%v", checkout, mapped, known)
		}
		got := paths.Resolve(execution.Directory{Kind: execution.DirectoryKnown, Path: execution.JoinPath(checkout, "SCRIPTS/nested")})
		if got.Kind != execution.DirectoryKnown || got.Path != want {
			t.Fatalf("checkout %q: directory=%+v, want %q", checkout, got, want)
		}
	}
}

func TestCompositeCheckoutAlternateRefMetadata(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo $VALUE
`)
	decoy := testutil.WriteFixture(t, root, "source/local/action.yml", `name: decoy
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo $DECOY
`)
	for _, destination := range []string{".", "source"} {
		for _, tc := range []struct {
			name, ref string
			read      bool
		}{
			{"default", "", true},
			{"empty expression", "${{ '' }}", true},
			{"literal tag", "v1", false},
			{"literal expression", "${{ 'v1' }}", false},
			{"literal SHA", "0123456789012345678901234567890123456789", false},
			{"dynamic ref", "${{ inputs.ref }}", true},
		} {
			t.Run(destination+"/"+tc.name, func(t *testing.T) {
				inputs := "path: " + destination
				if tc.ref != "" {
					inputs += ", ref: \"" + tc.ref + "\""
				}
				steps := "- uses: actions/checkout@v6\n  with: {" + inputs + "}\n- uses: ./" + filepath.ToSlash(filepath.Join(destination, "local"))
				result := testutil.Composite(t, root, steps, application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
				read := slices.Contains(result.Inputs, metadata) || slices.Contains(result.Inputs, decoy)
				if read != tc.read {
					t.Fatalf("metadata read=%v, want %v: %v", read, tc.read, result.Inputs)
				}
				wantMetadata := metadata
				if destination == "source" && tc.name == "dynamic ref" {
					// Unknown refs intentionally retain literal, untranslated fallback.
					wantMetadata = decoy
				}
				found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
					return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == wantMetadata && strings.Contains(d.Message, "SC2086")
				})
				if found != tc.read {
					t.Fatalf("current-tree script checked=%v, want %v: %+v", found, tc.read, result.Diagnostics)
				}
			})
		}
	}
	result := testutil.Composite(t, root, `- uses: actions/checkout@v6
  with: {ref: v1}
- uses: $/local`, application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if !slices.Contains(result.Inputs, metadata) {
		t.Fatalf("independent action metadata lost after alternate checkout: %v", result.Inputs)
	}
}
