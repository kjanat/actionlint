package checking

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestRemoteMetadataAdvisoriesUseEachInvocationBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, first string
		suppressed  bool
		want        []string
	}{
		{"different severities", "warn", false, []string{"error", "warning"}},
		{"one disabled parent", "off", false, []string{"error"}},
		{"source suppression", "warn", true, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			local := "name: parent\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: example/shared@v1\n"
			writeShellcheckFixture(t, root, "first/action.yml", local)
			writeShellcheckFixture(t, root, "second/action.yml", local)
			comment := ""
			if test.suppressed {
				comment = "    # actionlint-ignore-next-line coercion -- intentional conversion\n"
			}
			remote := "name: shared\ndescription: test\nruns:\n  using: composite\n  steps:\n" + comment + "    - if: startsWith(true, 'true')\n      shell: bash\n      run: echo ok\n"
			provider, requests := remoteCompositeProvider(t, map[string]string{"example/shared": remote})
			config, err := ConfigurationRules().ParseConfig([]byte("lint: {rules: {suspicious: {coercion: off}}}\noverrides:\n  - includes: ['first/**']\n    lint: {rules: {suspicious: {coercion: " + test.first + "}}}\n  - includes: ['second/**']\n    lint: {rules: {suspicious: {coercion: error}}}\n"))
			if err != nil {
				t.Fatal(err)
			}
			workflow := "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/first\n      - uses: $/second\n      - uses: $/first\n"
			result, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider}, []workspace.InputFile{{Path: "ci.yml", Project: workspace.Identity(root), Config: config, Content: []byte(workflow)}})
			if err != nil {
				t.Fatal(err)
			}
			var severities []string
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "coercion" {
					if !strings.Contains(diagnostic.Path, "example/shared/blob/") {
						t.Fatalf("advisory has wrong source: %+v", diagnostic)
					}
					severities = append(severities, string(diagnostic.Severity))
				}
				if diagnostic.Rule == "unused-suppression" {
					t.Fatalf("used suppression was reported unused: %+v", diagnostic)
				}
			}
			slices.Sort(severities)
			if !slices.Equal(severities, test.want) {
				t.Fatalf("coercion severities=%v, want %v; diagnostics=%+v", severities, test.want, result.Diagnostics)
			}
			if got := requests()["example/shared"]; got != 1 {
				t.Fatalf("remote metadata requested %d times, want 1", got)
			}
		})
	}
}
