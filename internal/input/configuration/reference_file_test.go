package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	"github.com/google/go-cmp/cmp"
)

func TestConfigGenerateDefaultConfigFileOK(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "default-config-for-test.yml")
	if err := checking.ConfigurationRules().WriteDefaultFile(f, "1.17.0"); err != nil {
		t.Fatal(err)
	}
	c, err := checking.ConfigurationRules().Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(repoconfig.RunnerLabels(c.Facts())) != 0 {
		t.Fatal(repoconfig.RunnerLabels(c.Facts()))
	}
	if repoconfig.VariableNames(c.Facts()) != nil {
		t.Fatal(repoconfig.RunnerLabels(c.Facts()))
	}
	if repoconfig.SecretNames(c.Facts()) != nil {
		t.Fatal(repoconfig.SecretNames(c.Facts()))
	}
	if len(c.Paths) != 0 {
		t.Fatal(c.Paths)
	}
	if _, known := c.Facts().DefaultWorkflowPermissions.Value(); known {
		t.Fatal("default config fabricated repository permissions")
	}
	if diff := cmp.Diff(repoconfig.Policy{}, c.Policy); diff != "" {
		t.Fatal(diff)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	const schemaHeader = "# yaml-language-server: $schema=https://cdn.jsdelivr.net/gh/kjanat/actionlint@1.17.0/actionlint.schema.json\n"
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
	if got := checking.ConfigurationRules().DiagnosticLevel(c, "missing-local-action"); got != "on" {
		t.Fatalf("generated config changes recommended rules: %s", got)
	}
	baseline, err := checking.ConfigurationRules().Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range checking.ConfigurationRules().RuleInfos() {
		if got, want := checking.ConfigurationRules().DiagnosticLevel(c, rule.Name), checking.ConfigurationRules().DiagnosticLevel(baseline, rule.Name); got != want {
			t.Errorf("generated config changes %s from %s to %s", rule.Name, want, got)
		}
	}
}

func TestConfigGenerateDefaultConfigFileError(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "missing", "test.yml")
	err := checking.ConfigurationRules().WriteDefaultFile(p, "1.17.0")
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not write default configuration file") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestConfigGenerateSnapshotSchemaHeader(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"2.0.0-SNAPSHOT-02d216d", "v2.0.0-rc.1-SNAPSHOT-02d216d"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := checking.ConfigurationRules().WriteDefaultFile(path, version); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			header, _, _ := strings.Cut(string(content), "\n")
			const want = "# yaml-language-server: $schema=https://cdn.jsdelivr.net/gh/kjanat/actionlint@02d216d/actionlint.schema.json"
			if header != want {
				t.Fatalf("snapshot schema header = %q, want %q", header, want)
			}
		})
	}
}
