package config_test

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestExplicitDirectoryFileScope(t *testing.T) {
	for _, tc := range []struct {
		config, path, directory string
		want                    bool
	}{
		{"files: {excludes: ['testdata/**']}", "testdata/ci.yml", "testdata", true},
		{"files: {excludes: ['testdata/**']}", "testdata/ci.yml", ".", false},
		{"files: {includes: ['**', '!testdata/**']}", "testdata/ci.yml", "testdata", true},
		{"files: {includes: ['**', '!testdata/**']}", "testdata/ci.yml", ".", false},
		{"files: {includes: ['.github/workflows/**', '!./testdata/**']}", "testdata/nested/ci.yml", "./testdata/nested", true},
		{"files: {includes: ['.github/workflows/**']}", "testdata/ci.yml", ".", false},
		{"files: {includes: ['.github/workflows/**']}", "testdata/ci.yml", "testdata", false},
		{"files: {includes: ['.github/workflows/**']}", ".github/workflows/ci.yml", ".", true},
		{"files: {includes: []}", "testdata/ci.yml", "testdata", false},
	} {
		cfg, err := repoconfig.Parse([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.IncludesSelectedFile(tc.path, ".", tc.directory); got != tc.want {
			t.Fatalf("%s: path=%s directory=%s got=%v want=%v", tc.config, tc.path, tc.directory, got, tc.want)
		}
	}
}
