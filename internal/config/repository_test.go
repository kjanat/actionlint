package config

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/repository"
)

func configuredWorkflowPermissions(cfg *Config) string {
	permissions, known := cfg.Facts().DefaultWorkflowPermissions.Value()
	if !known {
		return ""
	}
	return permissions.String()
}

func TestRepositoryFactsAreIndependentOfRules(t *testing.T) {
	cfg, err := Parse([]byte(`repository:
  default-workflow-permissions: write
  runners: [gpu, custom-*]
  variables: {PROVENANCE: "true", NAME_ONLY: null}
  secrets: []
lint: {rules: {preset: none}}
`))
	if err != nil {
		t.Fatal(err)
	}
	values, err := EffectiveValues(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository, ok := values["repository"].(map[string]any)
	if !ok || repository["default-workflow-permissions"] != "write" {
		t.Fatalf("repository facts disappeared when rules were disabled: %#v", values["repository"])
	}
}

func TestRepositoryMigrationDoesNotCreateRuleOptions(t *testing.T) {
	result, err := MigrateDocument("config.yaml", []byte("self-hosted-runner: {labels: [gpu]}\nconfig-variables: [BUILD]\nconfig-secrets: []\nassume-default-permissions: permissive\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"repository:", "default-workflow-permissions: write", "runners:", "variables:", "secrets: []"} {
		if !strings.Contains(string(result.Content), want) {
			t.Errorf("missing %q in migration:\n%s", want, result.Content)
		}
	}
	if strings.Contains(string(result.Content), "options:") {
		t.Fatalf("repository facts still depend on rule settings:\n%s", result.Content)
	}
}

func TestRepositoryRejectsInvalidFacts(t *testing.T) {
	for _, source := range []string{
		"repository: {default-workflow-permissions: restricted}",
		"repository: {default-workflow-permissions: true}",
		"repository: {variables: {BUILD: 42}}",
		"repository: {secrets: [TOKEN, token]}",
		"repository: {runners: ['[']}",
		"repository: {secret: [TOKEN]}",
		"repository: {runners: [gpu], runners: []}",
		"repository: {environments: {prod: {secrets: [TOKEN], secrets: []}}}",
		"repository: {variables: {BUILD: null, build: null}}",
	} {
		t.Run(source, func(t *testing.T) {
			if _, err := Parse([]byte(source)); err == nil {
				t.Fatal("invalid repository facts accepted")
			}
		})
	}
}

func TestRepositoryOverrideAvailability(t *testing.T) {
	names, err := repository.NewNames("HOST")
	if err != nil {
		t.Fatal(err)
	}
	host := repository.Snapshot{Secrets: names}
	for _, test := range []struct {
		source string
		known  bool
		names  []string
	}{
		{"{}", true, []string{"HOST"}},
		{"repository: null", false, nil},
		{"repository: {secrets: null}", false, nil},
		{"repository: {secrets: []}", true, []string{}},
		{"repository: {secrets: [CONFIG]}", true, []string{"CONFIG"}},
	} {
		t.Run(test.source, func(t *testing.T) {
			cfg, err := Parse([]byte(test.source))
			if err != nil {
				t.Fatal(err)
			}
			fact, known := RepositoryFacts(cfg, host).Secrets.Value()
			if known != test.known || known && !reflect.DeepEqual(fact.Values(), test.names) {
				t.Fatalf("override lost absence/reset/empty: %+v", cfg.Repository)
			}
		})
	}
}

func TestRepositoryResetSurvivesPartialFileOverride(t *testing.T) {
	permissions, err := repository.ParsePermissions("write")
	if err != nil {
		t.Fatal(err)
	}
	host := repository.Snapshot{DefaultWorkflowPermissions: permissions}
	cfg, err := Parse([]byte("repository: null\noverrides:\n  - includes: [release.yml]\n    repository: {secrets: [TOKEN]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	effective, err := ForFile(cfg, "release.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	facts := RepositoryFacts(effective, host)
	if _, known := facts.DefaultWorkflowPermissions.Value(); known {
		t.Fatal("partial override resurrected a reset host fact")
	}
	if names, known := facts.Secrets.Value(); !known || !reflect.DeepEqual(names.Values(), []string{"TOKEN"}) {
		t.Fatal("partial override lost its explicit value")
	}
}
