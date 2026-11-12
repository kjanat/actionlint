package config

import (
	"path/filepath"
	"testing"
)

func TestInheritedConfigDirectoryLateBoundPaths(t *testing.T) {
	t.Parallel()
	base := filepath.Join(t.TempDir(), "base")
	for _, expression := range []string{
		"${{ GITDIR }}", "${{  github.workspace }}", "${{github.action_path}}",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			input := expression + "/${{ CONFIGDIR }}/${{\tconfigdir\t}}/.shellcheckrc"
			want := expression + "/" + base + "/" + base + "/.shellcheckrc"
			if got := expandInheritedConfigDirectory(input, base); got != want {
				t.Fatalf("expanded path = %q; want %q", got, want)
			}
		})
	}
}
