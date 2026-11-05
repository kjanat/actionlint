package config_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestCommitHashExceptionsPreserveOtherFindings(t *testing.T) {
	cfg, err := checking.ConfigurationRules().Parse([]byte(`lint:
  rules:
    policy:
      require-commit-hash:
        level: true
        options: {exclude: ['my-org/*']}
policy:
  require-action-metadata: true
`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.Analyze(context.Background(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Config: cfg, Content: []byte(`on: push
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
	pins, metadataUnavailable := 0, false
	for _, d := range result.Diagnostics {
		if d.Rule == "unpinned-reference" {
			pins++
			if strings.Contains(d.Message, "my-org/") {
				t.Fatal(d.Message)
			}
		}
		metadataUnavailable = metadataUnavailable || d.Rule == "missing-action-metadata"
	}
	if pins != 2 || !metadataUnavailable {
		t.Fatalf("pins=%d metadata-unavailable=%v: %+v", pins, metadataUnavailable, result.Diagnostics)
	}
}
