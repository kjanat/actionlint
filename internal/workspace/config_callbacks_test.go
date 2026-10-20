package workspace_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestConfigLoadedCallbackOncePerConcurrentProject(t *testing.T) {
	overlay, err := repoconfig.ParseOverlay("config-variables", []byte("[ALLOWED]"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	session, err := workspace.New(workspace.Options{
		ConfigOverlays: []repoconfig.Overlay{overlay},
		OnConfigLoaded: func(repoconfig.Report) { calls.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	projects := []*workspace.Project{workspace.Identity("first"), workspace.Identity("second")}
	type loaded struct {
		project int
		config  *repoconfig.Config
		err     error
	}
	const requests = 16
	results := make(chan loaded, requests)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range requests {
		group.Go(func() {
			<-start
			project := i % len(projects)
			config, err := session.ConfigForProject(projects[project])
			results <- loaded{project, config, err}
		})
	}
	close(start)
	group.Wait()
	close(results)
	configs := make(map[int]*repoconfig.Config)
	for result := range results {
		if result.err != nil || result.config == nil || !reflect.DeepEqual(result.config.ConfigVariables, []string{"ALLOWED"}) {
			t.Fatalf("concurrent configuration: %+v", result)
		}
		if previous, ok := configs[result.project]; ok && previous != result.config {
			t.Fatal("concurrent readers received different configurations for the same project")
		}
		configs[result.project] = result.config
	}
	if got := calls.Load(); got != int32(len(projects)) {
		t.Fatalf("callback invoked %d times for %d projects", got, len(projects))
	}
}

func TestConfigOverlayResetOrigins(t *testing.T) {
	for _, tc := range []struct {
		input, value, source, state string
	}{
		{"config", "null", "input", "null"},
		{"policy", "null", "input", "null"},
		{"policy", "{}", "config", "value"},
	} {
		t.Run(tc.input+"/"+tc.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("policy: {require-commit-hash: true}"), 0o600); err != nil {
				t.Fatal(err)
			}
			overlay, err := repoconfig.ParseOverlay(tc.input, []byte(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			var report repoconfig.Report
			session, err := workspace.New(workspace.Options{ConfigFile: path, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(r repoconfig.Report) { report = r }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			origin := report.Inspection.Origins["/policy/require-commit-hash"]
			if origin.Source != tc.source || origin.State != tc.state || (tc.source == "input" && origin.Input != tc.input) {
				t.Fatalf("reset origin lost: %+v", origin)
			}
			if cfg.RequiresCommitHash() != (tc.value == "{}") {
				t.Fatalf("reset changed analysis semantics: %+v", cfg.Policy)
			}
		})
	}
}

func TestConfigOverlayPartialMappingAfterReset(t *testing.T) {
	for _, reset := range []string{"null", "policy: null"} {
		t.Run(reset, func(t *testing.T) {
			var overlays []repoconfig.Overlay
			for _, input := range []struct{ name, value string }{
				{"config", reset},
				{"policy", "cache-operation: false"},
				{"policy", "required-actions: []"},
			} {
				overlay, err := repoconfig.ParseOverlay(input.name, []byte(input.value))
				if err != nil {
					t.Fatal(err)
				}
				overlays = append(overlays, overlay)
			}
			var report repoconfig.Report
			session, err := workspace.New(workspace.Options{ConfigOverlays: overlays, OnConfigLoaded: func(r repoconfig.Report) { report = r }})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			origin := report.Inspection.Origins["/policy/require-commit-hash"]
			if origin.Source != "input" || origin.Input != "config" || origin.State != "null" || cfg.RequiresCommitHash() {
				t.Fatalf("lost reset after partial mapping: %+v", report.Inspection)
			}
			origin = report.Inspection.Origins["/policy/cache-operation"]
			if origin.Source != "input" || origin.Input != "policy" || origin.State != "value" || cfg.CachePolicyEnabled("cache-operation") {
				t.Fatalf("lost setting after reset: %+v", report.Inspection)
			}
		})
	}
}
