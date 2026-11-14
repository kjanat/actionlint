package config

import (
	"testing"

	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

func TestRepositoryServiceOverridesHostFact(t *testing.T) {
	t.Parallel()
	host, err := services.Parse("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		input   any
		kind    services.Kind
		version string
	}{
		{"inherit", map[string]any{}, services.GitHubCom, ""},
		{"clear service", map[string]any{"service": nil}, services.Unknown, ""},
		{"explicit unknown service", map[string]any{"service": "unknown"}, services.Unknown, ""},
		{"clear repository", nil, services.Unknown, ""},
		{"enterprise", map[string]any{"service": "ghes"}, services.GitHubEnterpriseServer, ""},
		{"enterprise version", map[string]any{"service": map[string]any{"kind": "ghes", "version": "3.19"}}, services.GitHubEnterpriseServer, "3.19"},
		{"data residency", map[string]any{"service": "github-data-residency"}, services.GitHubDataResidency, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var config RepositoryConfig
			if err := config.DecodeConfig(tc.input); err != nil {
				t.Fatal(err)
			}
			facts := config.Apply(repository.Snapshot{Service: host})
			if facts.Service.Kind() != tc.kind || facts.Service.Version() != tc.version {
				t.Fatalf("service = %v, want %s %s", facts.Service, tc.kind, tc.version)
			}
			encoded, err := config.EncodeConfig()
			if err != nil {
				t.Fatal(err)
			}
			var decoded RepositoryConfig
			if err := decoded.DecodeConfig(encoded); err != nil {
				t.Fatal(err)
			}
			if next := decoded.Apply(repository.Snapshot{Service: host}); next.Service != facts.Service {
				t.Fatalf("encoded service changed from %v to %v", facts.Service, next.Service)
			}
		})
	}
}

func TestRepositoryServiceRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, input := range []any{
		42, "github", "",
		[]any{"github.com"},
		map[string]any{},
		map[string]any{"kind": "ghes", "version": "latest"},
		map[string]any{"kind": "ghes", "version": "3"},
		map[string]any{"kind": "github.com", "version": "3.19"},
		map[string]any{"kind": "ghes", "typo": true},
	} {
		var config RepositoryConfig
		if err := config.DecodeConfig(map[string]any{"service": input}); err == nil {
			t.Fatalf("accepted service %#v", input)
		}
	}
}
