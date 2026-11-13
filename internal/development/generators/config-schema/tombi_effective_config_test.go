package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	options "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
)

func TestEffectiveConfigInheritance(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs("testdata/tombi/effective")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := config.NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := rs.Read(filepath.Join(dir, "main.toml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := loaded.Config
	facts := c.Facts()
	if got := config.RunnerLabels(facts); !reflect.DeepEqual(got, []string{"second"}) {
		t.Errorf("runners: %v", got)
	}
	if got := config.SecretNames(facts); !reflect.DeepEqual(got, []string{"MAIN"}) {
		t.Errorf("secrets: %v", got)
	}
	if got := config.VariableNames(facts); !reflect.DeepEqual(got, []string{"SECOND"}) {
		t.Errorf("variables: %v", got)
	}
	if !c.Files.Match("a.yml") || c.Files.Match("b.txt") {
		t.Error("inherited file selection is incorrect")
	}
	if got := c.LocalActions["./dist"]; got != "./second" {
		t.Errorf("normalized local action: %q", got)
	}
	envs, ok := facts.Environments.Value()
	if !ok || len(envs.Entries()) != 1 {
		t.Fatal("expected one environment")
	}
	env := envs.Entries()[0]
	if got := env.Secrets.Values(); !reflect.DeepEqual(got, []string{"SECOND"}) {
		t.Errorf("environment secrets: %v", got)
	}
	vars := env.Variables.Entries()
	if len(vars) != 1 {
		t.Fatalf("environment variables: %v", vars)
	}
	value, ok := vars[0].Value.Value()
	if vars[0].Name != "REGION" || !ok || value != "east" {
		t.Errorf("environment variable: %v", vars[0])
	}
	sources := loaded.Provenance.Sources()
	if len(sources) != 3 {
		t.Fatalf("sources: %v", sources)
	}
	for i, want := range []string{"first.toml", "second.toml", "main.toml"} {
		if !strings.HasSuffix(sources[i], want) {
			t.Errorf("source %d: %q, want %q", i, sources[i], want)
		}
	}
	for key, want := range map[string]string{"/repository/runners": "second.toml", "/repository/secrets": "main.toml", "/repository/environments/prod/variables/REGION": "second.toml", "/files/includes": "first.toml"} {
		if got := loaded.Origins[key].File; !strings.HasSuffix(got, want) {
			t.Errorf("origin %s: %q, want %q", key, got, want)
		}
	}
	for _, tc := range []struct{ path, runner, local string }{{"a.yml", "override-last", "./override-last"}, {"b.yml", "second", "./second"}} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			selected, err := rs.ForFile(c, tc.path, dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := config.RunnerLabels(selected.Facts()); !reflect.DeepEqual(got, []string{tc.runner}) {
				t.Errorf("runners: %v", got)
			}
			if got := selected.LocalActions["./dist"]; got != tc.local {
				t.Errorf("local action: %q", got)
			}
		})
	}
}

type effectiveRuleCase struct {
	path, action, nursery, pin, permission, timeout, suppression, excluded, location, report string
	min, max                                                                                 float64
}

func TestEffectiveJSONRoots(t *testing.T) {
	t.Parallel()
	rs, err := config.NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := "testdata/tombi"
	empty, err := rs.Read(filepath.Join(dir, "repository-empty.toml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyValues, err := rs.EffectiveValues(empty.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"null", "array", "string", "number", "boolean"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			loaded, err := rs.Read(filepath.Join(dir, "repository-json-root-"+name+".json"), nil)
			if name != "null" {
				if err == nil {
					t.Fatal("loader accepted a non-object root")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			values, err := rs.EffectiveValues(loaded.Config)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(values, emptyValues) {
				t.Errorf("null root produced different effective values: got %v, want %v", values, emptyValues)
			}
		})
	}
}

func TestEffectiveRuleOverrides(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("testdata/tombi/effective/rules.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rs := checking.ConfigurationRules()
	cfg, err := rs.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	cases := []effectiveRuleCase{
		{"outer/a.yml", "warn", "warn", "warn", "error", "on", "on", "root/a", "workflow", "suppression", 5, 60},
		{"inner/a.yml", "off", "off", "error", "warn", "warn", "warn", "inner/a", "every-job", "all", 5, 30},
		{"inner/re-enable/a.yml", "off", "warn", "error", "warn", "warn", "warn", "inner/a", "every-job", "all", 5, 30},
		{"inner/special/a.yml", "error", "off", "error", "warn", "off", "warn", "inner/a", "every-job", "all", 10, 30},
	}
	for _, want := range cases {
		t.Run(want.path, func(t *testing.T) {
			t.Parallel()
			selected, err := rs.ForFile(cfg, want.path, "")
			if err != nil {
				t.Fatal(err)
			}
			selected, err = rs.ResolveRules(config.Presets{}, selected)
			if err != nil {
				t.Fatal(err)
			}
			level := func(n string) string { return string(rs.DiagnosticLevel(selected, n)) }
			pin, ok := selected.Lint.Rules.Security.Rule("unpinned-reference")
			if !ok {
				t.Fatal("missing unpinned-reference")
			}
			pinOpt := new(options.CommitHashOptions)
			if _, err := pin.LoadOptions(pinOpt); err != nil {
				t.Fatal(err)
			}
			timeout, ok := selected.Lint.Rules.Conventions.Rule("job-timeout")
			if !ok {
				t.Fatal("missing job-timeout")
			}
			timeoutOpt := new(config.JobTimeoutPolicy)
			if _, err := timeout.LoadOptions(timeoutOpt); err != nil {
				t.Fatal(err)
			}
			minimum, _ := timeoutOpt.MinMinutes()
			maximum, _ := timeoutOpt.MaxMinutes()
			suppress, ok := selected.Lint.Rules.Conventions.Rule("forbidden-suppression")
			if !ok {
				t.Fatal("missing forbidden-suppression")
			}
			suppressOpt := new(config.SuppressionsPolicy)
			if _, err := suppress.LoadOptions(suppressOpt); err != nil {
				t.Fatal(err)
			}
			locations, err := selected.PermissionDeclarationLocations()
			if err != nil {
				t.Fatal(err)
			}
			location := ""
			if len(locations) > 0 {
				location = string(locations[0])
			}
			got := effectiveRuleCase{want.path, level("action"), level("errexit-status"), level("unpinned-reference"), level("missing-permissions"), level("job-timeout"), level("forbidden-suppression"), want.excluded, location, suppressOpt.Report(), minimum, maximum}
			if got != want {
				t.Errorf("effective settings: got %+v, want %+v", got, want)
			}
			other := "root/a"
			if want.excluded == other {
				other = "inner/a"
			}
			if !pinOpt.Excludes(want.excluded) || pinOpt.Excludes(other) {
				t.Errorf("exclusions: expected %q to match and %q not to match", want.excluded, other)
			}
		})
	}
}
