package config_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
)

func TestLiteralFileSelectionScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		config, path string
		explicit     bool
		want         bool
	}{
		{"files: {excludes: ['testdata/**']}", "testdata/ci.yml", true, true},
		{"files: {excludes: ['testdata/**']}", "testdata/ci.yml", false, false},
		{"files: {includes: ['**', '!testdata/**']}", "testdata/ci.yml", true, true},
		{"files: {includes: ['**', '!testdata/**']}", "testdata/ci.yml", false, false},
		{"files: {includes: ['.github/workflows/**']}", "testdata/ci.yml", true, true},
		{"files: {includes: ['.github/workflows/**']}", "testdata/ci.yml", false, false},
		{"files: {includes: ['.github/workflows/**']}", ".github/workflows/ci.yml", false, true},
		{"files: {includes: []}", "testdata/ci.yml", false, false},
		{"files: {includes: []}", "testdata/ci.yml", true, true},
	} {
		cfg, err := checking.ConfigurationRules().Parse([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.IncludesSelectedFile(tc.path, ".", tc.explicit); got != tc.want {
			t.Fatalf("%s: path=%s explicit=%v got=%v want=%v", tc.config, tc.path, tc.explicit, got, tc.want)
		}
	}
}
