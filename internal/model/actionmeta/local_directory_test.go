package actionmeta

import "testing"

func TestNormalizeLocalActionDirectory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, want string }{
		{"./source", "./source"},
		{"$/source", "./source"},
		{"./source/../action", "./action"},
		{"$/generated//nested/..", "./generated"},
		{"./.", "./"},
		{"$/", "./"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeLocalActionDirectory(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("NormalizeLocalActionDirectory(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
			}
		})
	}
	for _, input := range []string{
		"",
		"source",
		"/absolute",
		"./../outside",
		"$/../../outside",
		"./${{ inputs.path }}",
		"./path\\child",
		"./nul\x00byte",
		"./line\nbreak",
		"./line\rbreak",
		"./C:/path",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got, err := NormalizeLocalActionDirectory(input); err == nil {
				t.Fatalf("NormalizeLocalActionDirectory(%q) = %q, want error", input, got)
			}
		})
	}
}
