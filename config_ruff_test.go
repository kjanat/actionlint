package actionlint

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestRuffConfiguration(t *testing.T) {
	for _, text := range []string{
		"tools: {ruff: false}",
		"tools: {ruff: {enabled: false, target-version: py312, select: [F, B], ignore: [F401]}}",
		"tools: {ruff: {select: []}}",
	} {
		cfg, err := ParseConfig([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseConfig(data)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.Ruff.Select != nil && again.Tools.Ruff.Select == nil {
			t.Fatal("empty selection was lost")
		}
	}
	for _, text := range []string{
		"tools: {ruff: {typo: true}}",
		"tools: {ruff: {enabled: wrong}}",
		"tools: {ruff: {target-version: '--fix'}}",
		"tools: {ruff: {select: ['F,--fix']}}",
		"tools: {ruff: {ignore: ['']}}",
	} {
		if _, err := ParseConfig([]byte(text)); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}

func TestRuffRequiredToolsOverrides(t *testing.T) {
	root := t.TempDir()
	path := writeShellcheckFixture(t, root, "actionlint.yml", "tools: {shellcheck: false}\nfiles: {excludes: [skip.yml]}\noverrides:\n  - includes: [disabled.yml]\n    lint: {rules: {external: {ruff: off}}}\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: path, Ruff: "ruff", Shellcheck: "shellcheck"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"skip.yml", false}, {"disabled.yml", false}} {
		tools, err := session.RequiredTools([]string{tc.path})
		if err != nil || tools.Ruff != tc.want || tools.Shellcheck {
			t.Fatalf("%s: %+v %v", tc.path, tools, err)
		}
	}
}
