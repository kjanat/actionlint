package workspace_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	configreport "actionlint.kjanat.dev/v2/config/report"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestConfigurationPublicationOwnsSnapshots(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, overlay := range []bool{false, true} {
			name := "content"
			if empty {
				name = "empty"
			}
			if overlay {
				name += "/overlay"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "actionlint.yml")
				if err := os.WriteFile(path, []byte(`config-variables: [SAFE]
self-hosted-runner:
  typo: ignored
tools:
  ruff:
    config:
      lint:
        per-file-ignores:
          '*.py': [F401]
      custom: [{rules: [F841]}]
      numeric: {1: [{rules: [F821]}]}
`), 0o600); err != nil {
					t.Fatal(err)
				}
				opts := workspace.Options{WorkingDir: dir, ConfigFile: path}
				if overlay {
					value, err := repoconfig.ParseOverlay("config-variables", []byte("[SAFE]"))
					if err != nil {
						t.Fatal(err)
					}
					opts.ConfigOverlays = []repoconfig.Overlay{value}
				}
				selectInput := func(session *workspace.Session) *workspace.Selection {
					t.Helper()
					var result *workspace.Selection
					var err error
					if empty {
						result, err = session.Empty()
					} else {
						result, err = session.ReadStdin(strings.NewReader("on: push\njobs: {}\n"), false)
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(result.Configurations) != 1 {
						t.Fatalf("configurations = %+v", result.Configurations)
					}
					return result
				}
				baseline, err := workspace.New(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				want := selectInput(baseline).Configurations[0]
				var callback configreport.Report
				opts.OnConfigLoaded = func(report configreport.Report) {
					callback = report
					mutateConfigurationReport(t, report, "callback")
				}
				session, err := workspace.New(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				first := selectInput(session)
				if !reflect.DeepEqual(first.Configurations[0], want) {
					t.Fatalf("callback changed the published configuration:\ngot %+v\nwant %+v", first.Configurations[0], want)
				}
				mutateConfigurationReport(t, first.Configurations[0], "result")
				second := selectInput(session)
				if !reflect.DeepEqual(second.Configurations[0], want) {
					t.Fatalf("result changed the cached configuration:\ngot %+v\nwant %+v", second.Configurations[0], want)
				}
				mutateConfigurationReport(t, callback, "retained-callback")
				if !reflect.DeepEqual(second.Configurations[0], want) {
					t.Fatal("retained callback changed a previously returned selection")
				}
				// A new cache entry must also see the unmodified resolved source.
				project := workspace.Identity(filepath.Join(dir, "other"))
				third, err := session.Content("ci.yml", nil, project, false)
				if err != nil {
					t.Fatal(err)
				}
				want.Project = project.RootDir()
				if len(third.Configurations) != 1 || !reflect.DeepEqual(third.Configurations[0], want) {
					t.Fatalf("publication changed the source reused by a new project: %+v", third.Configurations)
				}
				cfg, err := session.ConfigForProject(nil)
				if err != nil || !reflect.DeepEqual(repoconfig.VariableNames(cfg.Facts()), []string{"SAFE"}) {
					t.Fatalf("publication changed analysis configuration: %+v, %v", cfg, err)
				}
			})
		}
	}
}

func mutateConfigurationReport(t *testing.T, report configreport.Report, value string) {
	t.Helper()
	if len(report.Overrides) != 0 {
		report.Overrides[0] = value
	}
	report.Inspection.Origins["/tools/ruff/config"] = configreport.Origin{Source: value}
	if len(report.Inspection.Warnings) != 1 {
		t.Fatalf("warnings = %+v", report.Inspection.Warnings)
	}
	report.Inspection.Warnings[0].Message = value
	config := report.Inspection.Config
	native := configurationMap(t, configurationMap(t, configurationMap(t, config["tools"])["ruff"])["config"])
	lint := configurationMap(t, native["lint"])
	configurationList(t, configurationMap(t, lint["per-file-ignores"])["*.py"])[0] = value
	entry := configurationMap(t, configurationList(t, native["custom"])[0])
	configurationList(t, entry["rules"])[0] = value
	numeric, ok := native["numeric"].(map[any]any)
	if !ok {
		t.Fatalf("non-string configuration keys decoded as %T", native["numeric"])
	}
	numericEntry := configurationMap(t, configurationList(t, numeric[1])[0])
	configurationList(t, numericEntry["rules"])[0] = value
	numeric[2] = value
	native["added"] = value
}

func configurationMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("configuration mapping has type %T", value)
	}
	return result
}

func configurationList(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("configuration list has type %T", value)
	}
	return result
}
