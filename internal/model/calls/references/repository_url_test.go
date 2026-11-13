package references

import "testing"

func TestRepositoryURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input, host, shorthand string
	}{
		{"https://codeberg.org/mlugg/setup-zig@v2", "codeberg.org", ""},
		{"https://github.com/mlugg/setup-zig@v2", "github.com", "mlugg/setup-zig@v2"},
		{"HTTPS://GitHub.com/mlugg/setup-zig/sub@feature/path", "GitHub.com", "mlugg/setup-zig/sub@feature/path"},
		{"https://github.com/mlugg/setup-zig@v2#", "github.com", ""},
		{"https://github.com/mlugg/setup-zig@v2#part", "github.com", ""},
		{"https://github.com/mlugg/setup-zig@v2?", "github.com", ""},
		{"https://github.com/mlugg/setup-zig@v2?download=1", "github.com", ""},
		{"https://github.com:443/mlugg/setup-zig@v2", "github.com", ""},
		{"https://user@github.com/mlugg/setup-zig@v2", "github.com", ""},
		{"https://github.com.evil.test/mlugg/setup-zig@v2", "github.com.evil.test", ""},
		{"https://github.com/mlugg/setup-zig@feature%2Fpath", "github.com", ""},
		{"https://github.com/mlugg/setup-zig", "github.com", ""},
		{"https://github.com/mlugg/setup-zig/../other@v2", "github.com", ""},
		{"http://github.com/mlugg/setup-zig@v2", "github.com", ""},
		{"mlugg/setup-zig@v2", "", ""},
		{"./action", "", ""},
		{"$/action", "", ""},
		{"docker://codeberg.org/team/image:v2", "", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			ref, present := ParseRepositoryURL(tc.input)
			if present != (tc.host != "") || ref.Host() != tc.host || ref.Shorthand() != tc.shorthand {
				t.Fatalf("got %+v/%t, want host=%q shorthand=%q", ref, present, tc.host, tc.shorthand)
			}
			if tc.shorthand != "" && tc.input[ref.PrefixLength():] != tc.shorthand {
				t.Fatal("prefix removal changed the repository, directory or ref")
			}
		})
	}
}
