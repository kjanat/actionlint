package displaypath

import (
	"path/filepath"
	"testing"
)

func TestFromDirectoryPicksShorterSpelling(t *testing.T) {
	t.Parallel()
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	join := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	working := join("home", "user", "projects", "app")
	for _, tc := range []struct{ name, directory, path, want string }{
		{"inside", working, join("home", "user", "projects", "app", ".github", "workflows", "ci.yml"), filepath.Join(".github", "workflows", "ci.yml")},
		{"sibling", working, join("home", "user", "projects", "other", "ci.yml"), filepath.Join("..", "other", "ci.yml")},
		{"far", working, join("var", "tmp", "case", ".github", "workflows", "t.yml"), join("var", "tmp", "case", ".github", "workflows", "t.yml")},
		{"tie", join("a", "b"), join("a", "c"), filepath.Join("..", "c")},
		{"relative input", working, filepath.Join("docs", "ci.yml"), filepath.Join("docs", "ci.yml")},
		{"relative directory", "relative", join("x.yml"), join("x.yml")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FromDirectory(tc.directory, tc.path); got != tc.want {
				t.Fatalf("FromDirectory(%q, %q) = %q, want %q", tc.directory, tc.path, got, tc.want)
			}
		})
	}
}
