package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references"
)

func writePolicy(t *testing.T, root, name, text string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNetworkPolicyDefaultsCanBeOverridden(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := writePolicy(t, root, "company.toml", "[defaults.network.metadata-mirrors]\nallow-unknown-repositories = true\n")
	for _, tc := range []struct {
		project string
		want    references.MirrorAccess
	}{
		{`{}`, references.AllowUnknownMirrors},
		{`{"network":{"metadata-mirrors":{"allow-unknown-repositories":false}}}`, references.PublicMirrorsOnly},
		{`{"network":{"metadata-mirrors":{"services":[]}}}`, references.NoMirrors},
	} {
		t.Run(tc.project, func(t *testing.T) {
			t.Parallel()
			policy, err := config.ReadNetworkPolicy([]string{base}, os.ReadFile)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := checking.ConfigurationRules().Parse([]byte(tc.project))
			if err != nil {
				t.Fatal(err)
			}
			applied, err := policy.Apply(cfg)
			if err != nil || applied.MetadataMirrorAccess() != tc.want {
				t.Fatalf("access = %v, error = %v; want %v", applied.MetadataMirrorAccess(), err, tc.want)
			}
		})
	}
}

func TestNetworkPolicyDefaultsAreIndependentOfReturnedConfigurations(t *testing.T) {
	t.Parallel()
	path := writePolicy(t, t.TempDir(), "policy.toml", "[defaults.network.metadata-mirrors]\nallow-unknown-repositories = true\n")
	policy, err := config.ReadNetworkPolicy([]string{path}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	first, err := policy.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	*first.Network.MetadataMirrors.AllowUnknownRepositories = false
	(*first.Network.MetadataMirrors.Services)[0] = "changed"
	second, err := policy.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.MetadataMirrorAccess() != references.AllowUnknownMirrors || !slices.Equal(*second.Network.MetadataMirrors.Services, config.MetadataMirrorServices{"jsdelivr"}) {
		t.Fatalf("mutating a returned configuration changed policy defaults: %+v", second.Network.MetadataMirrors)
	}
}

func TestNetworkPolicyRestrictionsCannotBeCleared(t *testing.T) {
	t.Parallel()
	for _, child := range []string{
		`{}`, `{"restrictions":null}`, `{"restrictions":{"network":null}}`,
		`{"restrictions":{"network":{"metadata-mirrors":null}}}`,
		`{"restrictions":{"network":{"metadata-mirrors":{"exclude-services":[],"exclude-repositories":[],"require-public":false}}}}`,
	} {
		t.Run(child, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			base := writePolicy(t, root, "company.toml", "[restrictions.network.metadata-mirrors]\nexclude-services = ['jsdelivr']\nexclude-repositories = ['github.com/company/private-*']\nrequire-public = true\n")
			text := `{"extends":["company.toml"],` + strings.TrimPrefix(child, "{")
			if child == `{}` {
				text = `{"extends":["company.toml"]}`
			}
			team := writePolicy(t, root, "team.json", text)
			policy, err := config.ReadNetworkPolicy([]string{team}, os.ReadFile)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := policy.Apply(nil)
			if err != nil || applied.MetadataMirrorAccess() != references.NoMirrors || !slices.Equal(applied.MetadataMirrorExclusions(), []string{"github.com/company/private-*"}) {
				t.Fatalf("settings = %+v, error = %v", applied, err)
			}
			for _, requested := range []string{`{"services":["jsdelivr"]}`, `{"allow-unknown-repositories":true}`} {
				cfg, err := checking.ConfigurationRules().Parse([]byte(`{"network":{"metadata-mirrors":` + requested + `}}`))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := policy.Apply(cfg); err == nil || !strings.Contains(err.Error(), strconv.Quote(base)) {
					t.Fatalf("request %s: wanted an error naming %s, got %v", requested, base, err)
				}
			}
		})
	}
}

func TestNetworkPolicyMergesIndependentFilesAndHTTPSParents(t *testing.T) {
	t.Parallel()
	const company = "https://settings.example.com/policy/company.toml"
	const team = "https://settings.example.com/policy/team.toml"
	const user = "https://settings.example.com/user.jsonc"
	documents := map[string]string{
		company: "[defaults.network.metadata-mirrors]\nallow-unknown-repositories = true\n[restrictions.network.metadata-mirrors]\nexclude-repositories = ['github.com/company/private-*']\n",
		team:    "extends = ['company.toml']\n[restrictions.network.metadata-mirrors]\nexclude-repositories = ['github.com/team/private-*']\n",
		user:    `{"defaults":{"network":{"metadata-mirrors":{"allow-unknown-repositories":false}}}}`,
	}
	policy, err := config.ReadNetworkPolicy([]string{team, user}, func(path string) ([]byte, error) {
		text, ok := documents[path]
		if !ok {
			return nil, fmt.Errorf("unexpected request: %s", path)
		}
		return []byte(text), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := policy.Apply(nil)
	if err != nil || cfg.MetadataMirrorAccess() != references.PublicMirrorsOnly {
		t.Fatalf("config = %+v, error = %v", cfg, err)
	}
	want := []string{"github.com/company/private-*", "github.com/team/private-*"}
	if !slices.Equal(cfg.MetadataMirrorExclusions(), want) {
		t.Fatalf("exclusions = %v, want %v", cfg.MetadataMirrorExclusions(), want)
	}
	if len(policy.Files()) != 3 {
		t.Fatalf("policy files = %v", policy.Files())
	}
}

func TestInvalidNetworkPolicyFails(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		`null`, `[]`, `{"typo":true}`, `{"extends":["missing.json"]}`, `{"extends":["policy.json"]}`,
		`{"restrictions":{"network":{"metadata-mirrors":{"require-public":"yes"}}}}`,
		`{"restrictions":{"network":{"metadata-mirrors":{"exclude-services":["typo"]}}}}`,
		`{"restrictions":{"network":{"metadata-mirrors":{"exclude-repositories":["["]}}}}`,
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			path := writePolicy(t, t.TempDir(), "policy.json", text)
			if _, err := config.ReadNetworkPolicy([]string{path}, os.ReadFile); err == nil {
				t.Fatalf("accepted policy %s", text)
			}
		})
	}
}

func TestNetworkPolicyInspectionReportsEffectiveSettingsAndOrigins(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := writePolicy(t, root, "policy.toml", "[defaults.network.metadata-mirrors]\nallow-unknown-repositories = true\n[restrictions.network.metadata-mirrors]\nexclude-services = ['jsdelivr']\n")
	policy, err := config.ReadNetworkPolicy([]string{path}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := checking.ConfigurationRules().InspectContextWithRegistrations(t.Context(), config.Selection{Disabled: true}, true, nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(inspected.Config)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := checking.ConfigurationRules().Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.MetadataMirrorAccess() != references.NoMirrors {
		t.Fatalf("config show reports enabled mirrors: %+v", inspected.Config)
	}
	for _, name := range []string{"services", "allow-unknown-repositories"} {
		origin := inspected.Origins["/network/metadata-mirrors/"+name]
		if origin.Source != "policy" || origin.File != path {
			t.Errorf("%s origin = %+v, want policy %s", name, origin, path)
		}
	}
}
