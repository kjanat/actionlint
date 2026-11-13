package shell

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"

	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
)

func TestPrepareRunVariables(t *testing.T) {
	t.Parallel()
	exclude := strings.Join([]string{"SC1091", "SC2194", "SC2050", "SC2157", "SC2043"}, "\n")
	dash := "dash"
	no := false
	for _, tc := range []struct {
		name string
		run  Run
		text string
		vars map[string]string
		// source is the prepared script.
		source string
	}{
		{
			name: "explicit bash", run: Run{Shell: Shell{Name: "bash"}, Sources: true, ConfigFile: "/rc"}, text: "echo $1",
			vars:   map[string]string{"dialect": "bash", "exclude": exclude, "config_file": "/rc", "external_sources": "true"},
			source: "set -e -o pipefail\necho $1\n",
		},
		{
			name: "unknown directory follows no sources", run: Run{Shell: Shell{Name: "bash", Implicit: true}}, text: "echo $1",
			vars:   map[string]string{"dialect": "bash", "exclude": exclude},
			source: "# shellcheck external-sources=false\nset -e\necho $1\n",
		},
		{
			name: "directive selects the dialect", run: Run{Shell: Shell{Name: "sh"}, Sources: true}, text: "# shellcheck shell=bash\necho $1",
			vars:   map[string]string{"exclude": exclude, "external_sources": "true"},
			source: "# shellcheck shell=bash\necho $1\n",
		},
		{
			name: "settings choose the dialect and refuse sources", run: Run{Shell: Shell{Name: "bash"}, Sources: true, Settings: &integrationconfig.Settings{Language: &dash, ReadAccess: integrationconfig.ReadAccess{Enabled: &no}}}, text: "echo ${{ x }}",
			vars:   map[string]string{"dialect": "dash", "exclude": exclude},
			source: "# shellcheck external-sources=false\necho ________\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run.Environment = scriptmodel.EnvironmentAlternatives(tc.run.Shell.Name, []runnerapi.OS{runnerapi.Linux})
			prepared, err := prepareShellcheck(t, tc.run, tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(prepared.Vars, tc.vars) || prepared.Source != tc.source || prepared.Config != nil {
				t.Fatalf("got vars %q, source %q, config %s", prepared.Vars, prepared.Source, prepared.Config)
			}
		})
	}
}

func TestPrepareRunLineOrigins(t *testing.T) {
	t.Parallel()
	prepared, err := prepareShellcheck(t, Run{Shell: Shell{Name: "bash"}, Settings: &integrationconfig.Settings{Prefix: "# shellcheck disable=SC2086\n"}}, "echo $1")
	if err != nil {
		t.Fatal(err)
	}
	want := []preparation.Line{{Origin: preparation.LineDirective}, {Origin: preparation.LineDirective}, {Origin: preparation.LineStartup}, {Line: 1, Origin: preparation.LineScript}}
	for row, line := range want {
		if got := prepared.LineMap(row + 1); got != line {
			t.Fatalf("row %d: got %+v, want %+v in\n%s", row+1, got, line, prepared.Source)
		}
	}
}

func TestPrepareRunSkipsUnknownDialects(t *testing.T) {
	t.Parallel()
	prepared, err := prepareShellcheck(t, Run{Shell: Shell{Name: "pwsh"}}, "Write-Host hi")
	if err != nil || prepared.Skip == "" || prepared.Source != "" {
		t.Fatalf("got %+v, %v", prepared, err)
	}
}

func TestPrepareRunForEngines(t *testing.T) {
	t.Parallel()
	prepared, err := prepareShellcheck(t, Run{Shell: Shell{Name: "sh"}, Sources: true, Engine: true}, "echo $1")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Shell           string   `json:"shell"`
		Exclude         []string `json:"exclude"`
		ExternalSources bool     `json:"external-sources"`
	}
	if err := json.Unmarshal(prepared.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.Shell != "sh" || len(config.Exclude) == 0 || config.ExternalSources || prepared.Vars != nil || !strings.HasPrefix(prepared.Source, "# shellcheck external-sources=false\n") {
		t.Fatalf("got config %s, vars %q, source %q", prepared.Config, prepared.Vars, prepared.Source)
	}
}

func TestEngineInputRefusesSources(t *testing.T) {
	t.Parallel()
	yes := true
	if _, err := prepareShellcheck(t, Run{Input: true, Engine: true, Language: "bash", Settings: &integrationconfig.Settings{ReadAccess: integrationconfig.ReadAccess{Enabled: &yes}}}, "echo $1"); !errors.Is(err, ErrNoSourceAccess) {
		t.Fatalf("got %v", err)
	}
}

func TestInvalidInvocationCannotBeOverriddenByDirective(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"bash -euo -pipefail {0}", "bash -O definitely_not_a_shopt {0}"} {
		prepared, err := prepareShellcheck(t, Run{Shell: Shell{Name: command}}, "# shellcheck shell=bash\necho hi\n")
		if err != nil || prepared.Skip == "" || prepared.Source != "" {
			t.Fatalf("prepared=%+v error=%v", prepared, err)
		}
	}
}
