package repoconfig

import (
	"path/filepath"
	"testing"
)

func TestConfigPathInterpolation(t *testing.T) {
	root := t.TempDir()
	context := PathContext{
		ConfigDir: filepath.Join(root, "config with spaces"),
		GitDir:    filepath.Join(root, "repo"), Workspace: root,
		ActionPath: filepath.Join(root, "repo", ".github", "actions", "example"),
	}
	for _, tc := range []struct{ value, want string }{
		{"${{ configdir }}/.shellcheckrc", filepath.Join(context.ConfigDir, ".shellcheckrc")},
		{"${{gitdir}}/scripts", filepath.Join(context.GitDir, "scripts")},
		{"${{ GITHUB.WORKSPACE }}/shared", filepath.Join(root, "shared")},
		{"${{ github.action_path }}/lib", filepath.Join(context.ActionPath, "lib")},
		{"./relative", filepath.Join(context.ConfigDir, "relative")},
	} {
		got, err := context.Resolve(tc.value)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	for _, value := range []string{"${{ env.HOME }}", "${{ configdir", "${{ format('x') }}", "{configdir}/rc", "{gitdir}/rc"} {
		if _, err := context.Resolve(value); err == nil {
			t.Errorf("accepted unsupported interpolation %q", value)
		}
	}
	context.ActionPath = ""
	if _, err := context.Expand("${{ github.action_path }}/lib"); err == nil {
		t.Error("invented action path outside an action context")
	}
	for _, literal := range []string{"literal-${{ env.HOME }}", "literal-{gitdir}"} {
		context.ConfigDir = filepath.Join(root, literal)
		got, err := context.Resolve("${{ configdir }}/lib")
		if err != nil || got != filepath.Join(context.ConfigDir, "lib") {
			t.Fatalf("replacement value was interpreted recursively: %q, %v", got, err)
		}
	}
}
