package engine

import (
	"context"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func TestCommitHashExceptionsPreserveOtherFindings(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`lint:
  rules:
    policy:
      require-commit-hash:
        level: true
        options: {exclude: ['my-org/*']}
`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runTestAnalysis(context.Background(), Request{
		WorkingDir: t.TempDir(),
		Sources: []SourceUnit{{Path: "ci.yml", Config: cfg, Content: []byte(`on: push
jobs:
  own:
    uses: my-org/repo/.github/workflows/ci.yml@main
  external:
    uses: other/repo/.github/workflows/ci.yml@main
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: my-org/action/subdir@main
      - uses: actions/checkout@v1
`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pins, retired := 0, false
	for _, d := range result.Diagnostics {
		if d.Rule == "require-commit-hash" {
			pins++
			if strings.Contains(d.Message, "my-org/") {
				t.Fatal(d.Message)
			}
		}
		retired = retired || d.Rule == "action" && strings.Contains(d.Message, "retired")
	}
	if pins != 2 || !retired {
		t.Fatalf("pins=%d retired=%v: %+v", pins, retired, result.Diagnostics)
	}
}
