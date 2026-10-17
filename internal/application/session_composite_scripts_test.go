package application

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/builtin"
	"actionlint.kjanat.dev/internal/tools/adapters"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestCompositeActionRuleSelection(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
	writeShellcheckFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout
`)
	for _, selection := range []string{"enabled", "removed", "empty"} {
		t.Run(selection, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: ./outer", analysisRequest{OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
				switch selection {
				case "removed":
					return slices.DeleteFunc(rules, func(rule workflowvisit.Rule) bool {
						_, action := rule.(*builtin.RuleAction)
						return action
					})
				case "empty":
					return nil
				default:
					return rules
				}
			}})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "action" })
			if found != (selection == "enabled") {
				t.Fatalf("action rule selection %q: %+v", selection, result.Diagnostics)
			}
		})
	}
}

func compositeAnalysis(t *testing.T, root, steps string, request analysisRequest) *analysisResult {
	t.Helper()
	workflow := writeShellcheckFixture(t, root, ".github/workflows/composite.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        shell: python
        working-directory: scripts
    steps:
      `+strings.ReplaceAll(steps, "\n", "\n      ")+"\n")
	request.WorkingDir = root
	session, err := workspace.New(workspace.Options{WorkingDir: root, ReadFile: request.ReadFile})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Sources = selection.Sources
	result, err := runTestAnalysis(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCompositeCheckoutMetadataPaths(t *testing.T) {
	root, _ := executableFixture(t)
	outer := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: ./source/bad.sh
    - uses: ./source/inner
`)
	inner := writeShellcheckFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - run: echo missing shell
`)
	decoy := writeShellcheckFixture(t, root, "source/local/action.yml", `name: wrong metadata
runs: {using: composite, steps: []}
`)
	for _, spec := range []string{"./source/local", "$/local"} {
		for _, removed := range []string{"", "executable-bit", "action"} {
			t.Run(spec+"/"+removed, func(t *testing.T) {
				result := compositeAnalysis(t, root, `- uses: actions/checkout@v6
  with: {path: source}
- uses: `+spec, analysisRequest{OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
					return slices.DeleteFunc(rules, func(rule workflowvisit.Rule) bool { return rule.Name() == removed })
				}})
				if !slices.Contains(result.Inputs, outer) || !slices.Contains(result.Inputs, inner) || slices.Contains(result.Inputs, decoy) {
					t.Fatalf("wrong metadata read: %v", result.Inputs)
				}
				for _, finding := range result.Diagnostics {
					if strings.Contains(finding.Message, "wrong metadata") {
						t.Fatalf("wrong metadata validated: %+v", finding)
					}
				}
				if removed != "executable-bit" && !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
					return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == outer
				}) {
					t.Fatalf("missing composite script finding: %+v", result.Diagnostics)
				}
				if removed != "action" && !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
					return d.Rule == "action" && filepath.Join(root, d.Path) == inner && strings.Contains(d.Message, "shell")
				}) {
					t.Fatalf("nested metadata validation lost: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeCheckoutShellcheckWithoutExecutableBit(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: source
      run: . ./lib.sh
`)
	writeShellcheckFixture(t, root, "lib.sh", "if then\n")
	writeShellcheckFixture(t, root, "source/lib.sh", "echo wrong file\n")
	result := compositeAnalysis(t, root, `- uses: actions/checkout@v6
  with: {path: source}
- uses: ./source/local`, analysisRequest{OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
		return slices.DeleteFunc(rules, func(rule workflowvisit.Rule) bool { return rule.Name() == "executable-bit" })
	}, Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata
	}) {
		t.Fatalf("composite ShellCheck read wrong source: %+v", result.Diagnostics)
	}
}

func TestCompositeSparseCheckoutSources(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: . ./lib.sh
    - shell: bash
      run: echo $UNQUOTED
`)
	writeShellcheckFixture(t, root, "lib.sh", "if then\n")
	for _, tc := range []struct {
		name, sparse string
		complete     bool
	}{
		{"sparse", "local", false},
		{"dynamic", "'${{ github.event.repository.name }}'", false},
		{"empty", "''", true},
		{"empty expression", "\"${{ '' }}\"", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {sparse-checkout: "+tc.sparse+"}\n- uses: ./local", analysisRequest{OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
				return slices.DeleteFunc(rules, func(rule workflowvisit.Rule) bool { return rule.Name() == "executable-bit" })
			}, Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("best-effort metadata validation lost: %v", result.Inputs)
			}
			inline, sourced := false, false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "shellcheck" || filepath.Join(root, diagnostic.Path) != metadata {
					continue
				}
				if strings.Contains(diagnostic.Message, "SC2086") {
					inline = true
				} else if strings.Contains(diagnostic.Message, "SC1094") {
					sourced = true
				}
			}
			if !inline || sourced != tc.complete {
				t.Fatalf("inline=%v, sourced=%v, complete=%v: %+v", inline, sourced, tc.complete, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutServerPaths(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ok
`)
	for _, tc := range []struct {
		name, server, spec string
		read               bool
	}{
		{"default server", "''", "./source/local", true},
		{"literal empty server", "\"${{ '' }}\"", "./source/local", true},
		{"alternate server", "https://git.example.com", "./source/local", false},
		{"unknown server", "'${{ inputs.server }}'", "./source/local", false},
		{"best effort metadata", "https://git.example.com", "./local", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: source, github-server-url: "+tc.server+"}\n- uses: "+tc.spec, analysisRequest{OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
				return slices.DeleteFunc(rules, func(rule workflowvisit.Rule) bool { return rule.Name() == "executable-bit" })
			}})
			if read := slices.Contains(result.Inputs, metadata); read != tc.read {
				t.Fatalf("metadata read = %v, want %v: %v", read, tc.read, result.Inputs)
			}
		})
	}
}
