package actionlint

import (
	"path/filepath"
	"testing"
)

func TestRunDirectoryContexts(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, value, action, checkout, want string
		known                               bool
	}{
		{"action", "${{ github.action_path }}", "local", "", "local", true},
		{"action child", "${{ github.action_path }}/scripts", "local", "", "local/scripts", true},
		{"checkout prefix", "${{ github.action_path }}/scripts", "local", "source", "source/local/scripts", true},
		{"workspace", "${{ github.workspace }}/scripts", "", "", "./scripts", true},
		{"missing action", "${{ github.action_path }}", "", "", "", false},
		{"outside action", "${{ github.action_path }}", "../other", "", "", false},
		{"unknown suffix", "${{ github.action_path }}/${{ inputs.dir }}", "local", "", "", false},
		{"unknown context", "${{ inputs.dir }}", "local", "", "", false},
		{"relative prefix", "prefix/${{ github.action_path }}", "local", "", "", false},
		{"config-only context", "${{ configdir }}", "local", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := runPaths{workspace: root, checkout: tc.checkout}
			if tc.action != "" {
				paths.actionPath = filepath.Join(root, tc.action)
			}
			got := paths.workingDirectory(&String{Value: tc.value})
			if (got.kind == directoryKnown) != tc.known || tc.known && got.path != tc.want {
				t.Fatalf("directory = %+v, want known=%v path=%q", got, tc.known, tc.want)
			}
		})
	}
}

func TestCompositeActionPathCasing(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "local/Nested/lib.sh", "VALUE=42\n")
	wantDirectory, err := filepath.EvalSymlinks(filepath.Join(root, "local", "Nested"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, checkout, spec, want string }{
		{"root", "", "./LOCAL/nested", "local/Nested"},
		{"placed", "Source", "./source/LOCAL/nested", "Source/local/Nested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := &LocalActionsCache{caseInsensitive: true}
			actions.restoreCheckout(&checkoutPlacement{directory: runDirectory{directoryKnown, tc.checkout}, caseInsensitive: true})
			paths := runPaths{workspace: root, analysis: root, platform: platformKindWindows, placements: actions.checkoutState()}
			call := &Step{Exec: &ExecAction{Uses: &String{Value: tc.spec}}}
			compositeActionOrigin(&paths, call, filepath.Join(root, "local", "Nested"))
			if paths.actionRunnerPath != tc.want {
				t.Fatalf("runner action path=%q, want %q", paths.actionRunnerPath, tc.want)
			}
			directory := paths.resolve(paths.workingDirectory(&String{Value: "${{ github.action_path }}"}))
			if directory.kind != directoryKnown || directory.path != wantDirectory {
				t.Fatalf("action source directory unresolved: %+v", directory)
			}
		})
	}
}
