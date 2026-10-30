package provider_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// minimal is the smallest valid process manifest; cases below patch it.
const minimal = `{
  "schema_version": 1,
  "id": "example.invalid/providers/lint",
  "version": "1.0.0",
  "role": "script",
  "scope": "source",
  "transport": "process",
  "rule": {"name": "lint", "description": "Lint", "category": "correctness", "subcategory": "shell", "maturity": "stable", "provider": "lint"},
  "preparation": "kjanat:actionlint/shell-preparation@0.2.0",
  "selectors": {"filename": "actionlint.sh"},
  "capabilities": {"fixes": false, "reported_sources": false},
  "process": {
    "entry": "lint",
    "args": ["--format=json", "{flag:dialect:--shell}", "{user-args}", "-"],
    "stdin": "source",
    "config": {"render": "none"},
    "output": {"format": "json", "transform": {"language": "jq", "inline": "[]"}}
  }
}`

func TestParseMinimalManifest(t *testing.T) {
	m, err := provider.Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if m.Rule.Name != "lint" || m.Transport != provider.TransportProcess || m.Process == nil || m.Process.Output.Format != provider.FormatJSON {
		t.Errorf("unexpected manifest %+v", m)
	}
}

func TestParseRejects(t *testing.T) {
	replace := func(from, to string) string {
		if !strings.Contains(minimal, from) {
			t.Fatalf("fixture does not contain %q", from)
		}
		return strings.Replace(minimal, from, to, 1)
	}
	for name, document := range map[string]string{
		"duplicate key":             replace(`"version": "1.0.0",`, `"version": "1.0.0", "version": "2.0.0",`),
		"unknown member":            replace(`"version": "1.0.0",`, `"version": "1.0.0", "extra": true,`),
		"schema version":            replace(`"schema_version": 1`, `"schema_version": 2`),
		"rule name":                 replace(`"name": "lint"`, `"name": "Lint Tool"`),
		"process without spec":      replace(`"transport": "process"`, `"transport": "wasm-core"`),
		"unknown placeholder":       replace(`"{user-args}"`, `"{env:HOME}"`),
		"brace in literal":          replace(`"--format=json"`, `"--format={json}"`),
		"json without transform":    replace(`, "transform": {"language": "jq", "inline": "[]"}`, ``),
		"sarif with transform":      replace(`"format": "json"`, `"format": "sarif@2.1.0"`),
		"transform digest and body": replace(`"inline": "[]"`, `"inline": "[]", "digest": "sha256:`+strings.Repeat("0", 64)+`"`),
		"bad digest":                replace(`"inline": "[]"`, `"digest": "sha256:ABC"`),
		"filename with directory":   replace(`"filename": "actionlint.sh"`, `"filename": "../actionlint.sh"`),
		"unknown severity":          replace(`"reported_sources": false}`, `"reported_sources": false, "severities": {"style": "hint"}}`),
		"invalid script input":      replace(`"selectors": {`, `"selectors": {"inputs": [{"action": "Owner/Repo", "input": "script", "language": "python"}], `),
		"template effects unversioned": replace(`"config": {"render": "none"}`,
			`"config": {"render": "none"}, "template_effects": {"E501": "line-length"}`),
		"version pattern groups": replace(`"config": {"render": "none"}`,
			`"config": {"render": "none"}, "version": {"args": ["--version"], "pattern": "^lint (\\d+)\\.(\\d+)"}`),
		"external config schema ref": replace(`"capabilities"`,
			`"config_schema": {"$ref": "https://example.invalid/schema.json"}, "capabilities"`),
		"arg rule with two kinds": replace(`"config": {"render": "none"}`,
			`"config": {"render": "none"}, "user_args": {"operands": false, "forbidden": [{"flag": "--fix", "prefix": "@", "reason": "no"}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := provider.Parse([]byte(document))
			if err == nil {
				t.Fatal("accepted invalid manifest")
			}
			t.Log(err)
		})
	}
}

func TestParseTemplateEffects(t *testing.T) {
	document := strings.Replace(minimal, `"config": {"render": "none"}`,
		`"config": {"render": "none"}, "version": {"args": ["--version"], "pattern": "^lint (\\S+)", "template_effects": "1.0.0"}, "template_effects": {"E501": "line-length"}`, 1)
	if _, err := provider.Parse([]byte(document)); err == nil {
		t.Fatal("accepted template effects without the template-effects contract")
	}
	document = strings.Replace(document, `"fixes": false`, `"template_contract": "kjanat:actionlint/template-effects@0.1.0", "fixes": false`, 1)
	if _, err := provider.Parse([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Parse([]byte(strings.Replace(document, `"line-length"`, `"reformatted"`, 1))); err == nil {
		t.Error("accepted an unknown template effect")
	}
}

func TestParseRejectsOversizedManifest(t *testing.T) {
	if _, err := provider.Parse([]byte(strings.Repeat(" ", 1<<20+1))); err == nil {
		t.Error("accepted manifest over the size limit")
	}
}
