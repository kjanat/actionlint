package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/buildinfo"

	"github.com/google/go-cmp/cmp"
)

func TestConfigGenerateDefaultConfigFileOK(t *testing.T) {
	previousVersion := buildinfo.ReleaseVersion
	buildinfo.ReleaseVersion = "1.17.0"
	t.Cleanup(func() { buildinfo.ReleaseVersion = previousVersion })
	f := filepath.Join(t.TempDir(), "default-config-for-test.yml")
	if err := writeDefaultConfigFile(f); err != nil {
		t.Fatal(err)
	}
	c, err := ReadConfigFile(f)
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
	if c.AssumeDefaultPermissions != DefaultPermissionsAssumptionUnset {
		t.Fatal(c.AssumeDefaultPermissions)
	}
	if diff := cmp.Diff(Policy{}, c.Policy); diff != "" {
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
	typ := reflect.TypeFor[Config]()
	for field := range typ.Fields() {
		if !field.IsExported() {
			continue
		}
		key, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if key != "" && key != "-" && !strings.Contains(string(b), key+":") {
			t.Errorf("generated reference omits %s", key)
		}
	}
	if strings.Index(string(b), "lint:") > strings.Index(string(b), "self-hosted-runner:") {
		t.Fatal("repository allowlists precede lint settings")
	}
	if got := diagnosticLevel(c, "missing-local-action"); got != "on" {
		t.Fatalf("generated config changes recommended rules: %s", got)
	}
	baseline, err := ParseConfig([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range BuiltinRules() {
		if got, want := diagnosticLevel(c, rule.Name), diagnosticLevel(baseline, rule.Name); got != want {
			t.Errorf("generated config changes %s from %s to %s", rule.Name, want, got)
		}
	}
}

func TestConfigGenerateDefaultConfigFileError(t *testing.T) {
	p := filepath.Join("testdata", "config", "dir-does-not-exist", "test.yml")
	err := writeDefaultConfigFile(p)
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not write default configuration file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}
