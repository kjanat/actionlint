package shell

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/profile"
)

func TestPrepareRunVariables(t *testing.T) {
	exclude := strings.Join(Excluded(nil), "\n")
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
			vars:   map[string]string{VarDialect: "bash", VarExclude: exclude, VarConfigFile: "/rc", VarExternalSources: "true"},
			source: "set -e -o pipefail\necho $1\n",
		},
		{
			name: "unknown directory follows no sources", run: Run{Shell: Shell{Name: "bash", Implicit: true}}, text: "echo $1",
			vars:   map[string]string{VarDialect: "bash", VarExclude: exclude},
			source: "# shellcheck external-sources=false\nset -e\necho $1\n",
		},
		{
			name: "directive selects the dialect", run: Run{Shell: Shell{Name: "sh"}, Sources: true}, text: "# shellcheck shell=bash\necho $1",
			vars:   map[string]string{VarExclude: exclude, VarExternalSources: "true"},
			source: "# shellcheck shell=bash\necho $1\n",
		},
		{
			name: "settings choose the dialect and refuse sources", run: Run{Shell: Shell{Name: "bash"}, Sources: true, Directives: &Directives{Shell: &dash, ExternalSources: &no}}, text: "echo ${{ x }}",
			vars:   map[string]string{VarDialect: "dash", VarExclude: exclude},
			source: "# shellcheck external-sources=false\necho ________\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := PrepareRun(tc.run, tc.text)
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
	prepared, err := PrepareRun(Run{Shell: Shell{Name: "bash"}, Directives: &Directives{Disable: []string{"SC2086"}}}, "echo $1")
	if err != nil {
		t.Fatal(err)
	}
	want := []profile.Line{{Origin: profile.LineDirective}, {Origin: profile.LineDirective}, {Origin: profile.LineStartup}, {Line: 1, Origin: profile.LineScript}}
	for row, line := range want {
		if got := prepared.LineMap(row + 1); got != line {
			t.Fatalf("row %d: got %+v, want %+v in\n%s", row+1, got, line, prepared.Source)
		}
	}
}

func TestPrepareRunSkipsUnknownDialects(t *testing.T) {
	prepared, err := PrepareRun(Run{Shell: Shell{Name: "pwsh"}}, "Write-Host hi")
	if err != nil || prepared.Skip == "" || prepared.Source != "" {
		t.Fatalf("got %+v, %v", prepared, err)
	}
}

func TestPrepareRunForEngines(t *testing.T) {
	prepared, err := PrepareRun(Run{Shell: Shell{Name: "sh"}, Sources: true, Engine: true}, "echo $1")
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
	yes := true
	if _, err := EngineInput("echo $1", "bash", &Directives{ExternalSources: &yes}); !errors.Is(err, ErrNoSourceAccess) {
		t.Fatalf("got %v", err)
	}
}
