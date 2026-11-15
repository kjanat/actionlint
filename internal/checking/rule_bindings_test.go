package checking

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestRemoteBodyKeepsEachParentsPreparedRuleDecision(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, first string
		suppressed  bool
		want        []string
	}{
		{"different severities", "warn", false, []string{"error", "warning"}},
		{"one disabled parent", "off", false, []string{"error"}},
		{"source suppression across parents", "warn", true, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			local := "name: parent\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: example/shared@v1\n"
			writeShellcheckFixture(t, root, "first/action.yml", local)
			writeShellcheckFixture(t, root, "second/action.yml", local)
			comment := ""
			if test.suppressed {
				comment = "    # actionlint-ignore-next-line container-runner -- known execution restriction\n"
			}
			remote := "name: shared\ndescription: test\nruns:\n  using: composite\n  steps:\n" + comment + "    - uses: docker://alpine:3\n"
			provider, _ := remoteCompositeProvider(t, map[string]string{"example/shared": remote})
			config, err := ConfigurationRules().Parse([]byte("lint: {rules: {correctness: {container-runner: off}}}\noverrides:\n  - includes: ['first/**']\n    lint: {rules: {correctness: {container-runner: " + test.first + "}}}\n  - includes: ['second/**']\n    lint: {rules: {correctness: {container-runner: error}}}\n"))
			if err != nil {
				t.Fatal(err)
			}
			owner := newRuleConfigurations(config, root, repoconfig.Presets{}, ConfigurationRules(), nil)
			entry, err := owner.workflow(filepath.Join(root, "ci.yml"))
			if err != nil {
				t.Fatal(err)
			}
			owner.bindEntry(filepath.Join(root, "ci.yml"), entry)
			for path, want := range map[string]string{"first": test.first, "second": "error"} {
				parent, err := owner.bindSource(filepath.Join(root, path, "action.yml"))
				if err != nil {
					t.Fatal(err)
				}
				decision := parent.Rule("container-runner")
				if got := ConfigurationRules().DiagnosticLevel(decision.Config(), "container-runner"); string(got) != want {
					t.Fatalf("%s execution level=%s, want %s", path, got, want)
				}
			}
			workflow := "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: $/first\n      - uses: $/second\n"
			result, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Config: config, Content: []byte(workflow)}})
			if err != nil {
				t.Fatal(err)
			}
			var severities []string
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "container-runner" {
					if !strings.Contains(diagnostic.Path, "example/shared/blob/") {
						t.Fatalf("wrong source: %+v", diagnostic)
					}
					severities = append(severities, diagnostic.Severity)
				}
				if diagnostic.Rule == "unused-suppression" {
					t.Fatalf("suppression used by another occurrence was removed: %+v", diagnostic)
				}
			}
			slices.Sort(severities)
			if !slices.Equal(severities, test.want) {
				t.Fatalf("severities=%v, want %v; diagnostics=%+v", severities, test.want, result.Diagnostics)
			}
		})
	}
}

func TestSourceBindingOwnerCachesLogicalAddresses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	owner := newRuleConfigurations(nil, root, repoconfig.Presets{}, ConfigurationRules(), nil)
	owner.bindEntry("ci.yml", nil)
	first, err := owner.bindSource("action.yml")
	if err != nil {
		t.Fatal(err)
	}
	again, err := owner.bindSource("action.yml")
	if err != nil || again != first {
		t.Fatalf("same logical address was rebound: %v", err)
	}
	alias, err := owner.bindSource("./action.yml")
	if err != nil || alias == first {
		t.Fatalf("different logical addresses shared policy identity: %v", err)
	}
}

func TestLocalMetadataAdvisoryUsesTargetBinding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: $/inner\n")
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - if: startsWith(true, 'true')\n      shell: bash\n      run: echo ok\n")
	config, err := ConfigurationRules().Parse([]byte("lint: {rules: {suspicious: {coercion: off}}}\noverrides:\n  - includes: ['inner/**']\n    lint: {rules: {suspicious: {coercion: error}}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/outer\n"
	result, err := Analyze(t.Context(), Options{WorkingDir: root}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Config: config, Content: []byte(workflow)}})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule == "coercion" {
			count++
			if diagnostic.Severity != "error" || !strings.Contains(filepath.ToSlash(diagnostic.Path), "inner/action.yml") {
				t.Fatalf("wrong metadata binding: %+v", diagnostic)
			}
		}
	}
	if count != 1 {
		t.Fatalf("wanted one target advisory, got %+v", result.Diagnostics)
	}
}
