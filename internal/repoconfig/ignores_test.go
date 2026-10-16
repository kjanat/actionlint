package repoconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReusableIgnoreConfig(t *testing.T) {
	for _, source := range []string{
		`ignores: [{rule: expression, uses: './generated', file: '.github/workflows/*.yml', job: build, step: lint, reason: generated}]`,
		`{"ignores":[{"rule":"expression","uses":"./generated","file":".github/workflows/*.yml","job":"build","step":"lint","reason":"generated"}]}`,
	} {
		cfg, err := Parse([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		want := IgnoreConfigs{{Rule: "expression", Uses: "./generated", File: ".github/workflows/*.yml", Job: "build", Step: "lint", Reason: "generated"}}
		if !reflect.DeepEqual(cfg.Ignores, want) {
			t.Fatalf("got %#v", cfg.Ignores)
		}
	}
	for _, entry := range []string{
		`{rule: expression}`, `{rule: unknown, uses: './generated'}`, `{rule: expression, file: '../outside'}`,
		`{rule: expression, file: '!foo'}`, `{rule: expression, file: '['}`, `{rule: expression, file: '/tmp/x'}`,
		`{rule: expression, step: ''}`, `{rule: expression, uses: './generated', reason: ''}`,
		`{rule: expression, uses: './generated', unknown: x}`, `{rule: expression, file: true}`,
		`{rule: expression, file: x, file: y}`, `{rule: expression, uses: null}`, `null`,
		`{rule: disallow-suppressions, file: '**'}`, `{rule: expression, job: ' '}`,
	} {
		t.Run(entry, func(t *testing.T) {
			if _, err := Parse([]byte("ignores: [" + entry + "]")); err == nil {
				t.Fatalf("accepted invalid ignore %s", entry)
			}
		})
	}
}

func TestReusableIgnoreInheritance(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base.yaml")
	child := filepath.Join(root, "child.yaml")
	if err := os.WriteFile(base, []byte("ignores: [{rule: expression, job: build}]"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		overlay string
		want    int
		rule    string
	}{
		{"", 1, "expression"}, {"ignores: []", 0, ""}, {"ignores: null", 0, ""},
		{"ignores: [{rule: action, uses: './generated'}]", 1, "action"},
	} {
		if err := os.WriteFile(child, []byte("extends: [base.yaml]\n"+tc.overlay), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(child)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Ignores) != tc.want || tc.want > 0 && cfg.Ignores[0].Rule != tc.rule {
			t.Fatalf("overlay %q: %#v", tc.overlay, cfg.Ignores)
		}
	}
}
