package engine

import (
	"context"
	"strings"
	"testing"
)

func TestCommitHashExceptionsPreserveOtherFindings(t *testing.T) {
	cfg, err := ParseConfig([]byte(`lint:
  rules:
    policy:
      require-commit-hash:
        level: true
        options: {exclude: ['my-org/*']}
`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Analyze(context.Background(), AnalysisRequest{
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

func TestNativeRuffConfigRoundTrip(t *testing.T) {
	cfg, err := ParseConfig([]byte(`tools:
  ruff:
    config:
      target-version: py312
      lint: {extend-select: [B, S], ignore: [F401]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Ruff.Native["target-version"] != "py312" {
		t.Fatal(cfg.Tools.Ruff.Native)
	}
}
