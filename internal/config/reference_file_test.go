package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"

	"github.com/google/go-cmp/cmp"
)

func TestConfigGenerateDefaultConfigFileOK(t *testing.T) {
	f := filepath.Join(t.TempDir(), "default-config-for-test.yml")
	if err := repoconfig.WriteDefaultFile(f, "1.17.0"); err != nil {
		t.Fatal(err)
	}
	c, err := repoconfig.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SelfHostedRunner.Labels) != 0 {
		t.Fatal(c.SelfHostedRunner.Labels)
	}
	if c.ConfigVariables != nil {
		t.Fatal(c.SelfHostedRunner.Labels)
	}
	if c.ConfigSecrets != nil {
		t.Fatal(c.ConfigSecrets)
	}
	if len(c.Paths) != 0 {
		t.Fatal(c.Paths)
	}
	if c.AssumeDefaultPermissions != repoconfig.DefaultPermissionsAssumptionUnset {
		t.Fatal(c.AssumeDefaultPermissions)
	}
	if diff := cmp.Diff(repoconfig.Policy{}, c.Policy); diff != "" {
		t.Fatal(diff)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	const schemaHeader = "# yaml-language-server: $schema=https://raw.githubusercontent.com/kjanat/actionlint/v1.17.0/actionlint.schema.json\n"
	if !strings.HasPrefix(string(b), schemaHeader) {
		t.Fatalf("generated config must start with the YAML Language Server schema directive, got %q", string(b))
	}
	if strings.Count(string(b), "\n---\n") != 1 || strings.Count(string(b), "# yaml-language-server: $schema=") != 1 {
		t.Fatal("generated config duplicates the schema header or document separator")
	}
	// Every public top-level setting must be discoverable in the reference.
	typ := reflect.TypeFor[repoconfig.Config]()
	for field := range typ.Fields() {
		if !field.IsExported() {
			continue
		}
		key, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if key != "" && key != "-" && !strings.Contains(string(b), key+":") {
			t.Errorf("generated reference omits %s", key)
		}
	}
	if strings.Index(string(b), "lint:") > strings.Index(string(b), "runner-label:") {
		t.Fatal("repository allowlists precede lint settings")
	}
	if got := repoconfig.DiagnosticLevel(c, "missing-local-action"); got != "on" {
		t.Fatalf("generated config changes recommended rules: %s", got)
	}
	baseline, err := repoconfig.Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range repoconfig.RuleInfos() {
		if got, want := repoconfig.DiagnosticLevel(c, rule.Name), repoconfig.DiagnosticLevel(baseline, rule.Name); got != want {
			t.Errorf("generated config changes %s from %s to %s", rule.Name, want, got)
		}
	}
}

func TestConfigGenerateDefaultConfigFileError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing", "test.yml")
	err := repoconfig.WriteDefaultFile(p, "1.17.0")
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not write default configuration file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}
