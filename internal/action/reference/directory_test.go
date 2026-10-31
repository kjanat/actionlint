package reference

import "testing"

func TestNormalizeLocalDirectory(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"./source", "./source"},
		{"$/source", "./source"},
		{"./source/../action", "./action"},
		{"$/generated//nested/..", "./generated"},
		{"./.", "./"},
		{"$/", "./"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeLocalDirectory(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("NormalizeLocalDirectory(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
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
			if got, err := NormalizeLocalDirectory(input); err == nil {
				t.Fatalf("NormalizeLocalDirectory(%q) = %q, want error", input, got)
			}
		})
	}
}
