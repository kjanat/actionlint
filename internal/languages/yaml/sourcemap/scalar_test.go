package sourcemap

import "testing"

func TestUnicodeEscapeBounds(t *testing.T) {
	for _, tc := range []struct {
		escape string
		want   string
		valid  bool
	}{
		{`\x00`, "\x00", true},
		{`\xFF`, "\u00ff", true},
		{`\uD7FF`, "\ud7ff", true},
		{`\uD800`, "", false},
		{`\uDFFF`, "", false},
		{`\uE000`, "\ue000", true},
		{`\U0001F600`, "😀", true},
		{`\U0010FFFF`, "\U0010ffff", true},
		{`\U00110000`, "", false},
		{`\U7FFFFFFF`, "", false},
		{`\U80000000`, "", false},
		{`\UFFFFFFFF`, "", false},
	} {
		t.Run(tc.escape, func(t *testing.T) {
			source := `"` + tc.escape + `"`
			got, err := NewDocument(source).Decode(Position{Line: 1, Column: 1}, Context{Indent: -1})
			if (err == nil) != tc.valid {
				t.Fatalf("Decode(%q) error = %v, valid = %v", source, err, tc.valid)
			}
			if tc.valid && got.Value != tc.want {
				t.Fatalf("Decode(%q) = %q, want %q", source, got.Value, tc.want)
			}
		})
	}
}
