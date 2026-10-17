package rules

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestCommitHashExclusions(t *testing.T) {
	for _, tc := range []struct {
		pattern, spec string
		want          bool
	}{
		{"my-org/*", "my-org/action@main", true},
		{"my-org/*", "my-org/repo/sub/action@main", true},
		{"my-org/*", "my-org/repo/.github/workflows/build.yml@main", true},
		{"my-org/*", "other-org/action@main", false},
		{"my-org/*", "my-org-other/action@main", false},
		{"my-org/action@v1", "my-org/action@v1", true},
		{"my-org/action@v1", "my-org/action@v2", false},
		{"my-org/repo/sub/*", "my-org/repo/other/action@v1", false},
		{"docker://alpine:*", "docker://alpine:3", true},
		{"docker://alpine:*", "docker://ubuntu:24.04", false},
	} {
		t.Run(tc.pattern+"/"+tc.spec, func(t *testing.T) {
			options := &CommitHashOptions{Exclude: []string{tc.pattern}}
			if got := options.Excludes(tc.spec); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	for _, input := range []string{"exclude: ['[']", "exclude: ['']", "excluded: [org/*]"} {
		var options CommitHashOptions
		if err := yaml.Unmarshal([]byte(input), &options); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
