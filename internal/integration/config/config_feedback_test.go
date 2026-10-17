package config_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
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
	result, err := application.Analyze(context.Background(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Config: cfg, Content: []byte(`on: push
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
	)
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
