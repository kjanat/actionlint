package config

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
)

func TestSettingsDTOsHaveNoYAMLMutationHooks(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{reflect.TypeFor[*Config](), reflect.TypeFor[*Policy]()} {
		if _, exists := typ.MethodByName("UnmarshalYAML"); exists {
			t.Fatalf("%s owns decoding instead of its wire adapter", typ)
		}
	}
	for field := range reflect.TypeFor[Config]().Fields() {
		if !field.IsExported() {
			t.Fatalf("settings DTO retains private state: %s", field.Name)
		}
	}
}

func TestConfigCodecPreservesProgrammaticProjectionsAndResets(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet([]rule.Declaration{{
		Name: "unpinned-reference", Aliases: []rule.Alias{{Name: "require-commit-hash", Deprecated: true}}, Category: "security", Maturity: "stable",
		Options: func() any { return &lintconfig.CommitHashOptions{} },
	}})
	if err != nil {
		t.Fatal(err)
	}
	initial := &Config{
		Paths:  map[string]PathConfig{"**/*.yml": {Ignore: IgnorePatterns{regexp.MustCompile("message")}}},
		Policy: Policy{RequireCommitHash: new(true)},
	}
	encoded, err := rules.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "\npolicy:") || strings.Contains(string(encoded), "\npaths:") {
		t.Fatalf("encoder retained legacy syntax: %s", encoded)
	}
	var decoded Config
	if err := rules.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.RequiresCommitHash() || !decoded.Paths["**/*.yml"].Ignore.Match("message") {
		t.Fatalf("programmatic projections were lost: %+v", decoded)
	}
	if err := rules.Unmarshal([]byte("repository: null\n"), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Repository.reset || decoded.RequiresCommitHash() || decoded.Paths != nil {
		t.Fatalf("fresh decoding lost reset or retained old settings: %+v", decoded)
	}
	resolved, err := rules.ResolveDocument([]byte("plugins: null\n"))
	if err != nil || resolved.Config.Plugins == nil {
		t.Fatalf("explicit no-plugins selection lost: %+v, %v", resolved, err)
	}
}

func TestConfigGetterResultsDetached(t *testing.T) {
	t.Parallel()
	permissions, err := RequirePermissions("workflow")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile("a|ab")
	cfg := &Config{
		Paths:  map[string]PathConfig{"**": {Ignore: IgnorePatterns{pattern}}},
		Policy: Policy{RequireJobTimeout: RequireJobTimeout(30), RequirePermissions: permissions},
	}
	timeout := cfg.RequiresJobTimeout()
	if timeout == nil {
		t.Fatal("missing timeout")
	}
	timeout.maxMinutes = 1
	declaration := cfg.RequiresPermissions()
	if declaration == nil {
		t.Fatal("missing permissions")
	}
	declaration.scope = "job"
	paths := cfg.PathConfigs("ci.yml")
	if len(paths) != 1 || len(paths[0].Ignore) != 1 {
		t.Fatal("missing path settings")
	}
	paths[0].Ignore[0].Longest()
	paths[0].Ignore[0] = regexp.MustCompile("changed")
	maximum, _ := cfg.RequiresJobTimeout().MaxMinutes()
	if maximum != 30 || cfg.RequiresPermissions().Scope() != "workflow" || pattern.FindString("ab") != "a" || !cfg.Paths["**"].Ignore.Match("ab") {
		t.Fatal("getter result mutated stored settings")
	}
}

func TestLoadedProvenanceSourcesDetached(t *testing.T) {
	t.Parallel()
	provenance := Provenance{path: "/root.yml", files: []string{"/base.yml", "/root.yml"}}
	sources := provenance.Sources()
	sources[0] = "changed"
	if provenance.Path() != "/root.yml" || !slices.Equal(provenance.Sources(), []string{"/base.yml", "/root.yml"}) {
		t.Fatal("provenance getter exposed source storage")
	}
}
