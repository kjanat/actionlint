package scripts

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestSelectionCopyPreservesCapabilitiesAndDetachesOptions(t *testing.T) {
	t.Parallel()
	executable := "original"
	backend := &selectorBackend{}
	calls := 0
	original := Selection{
		Process: &processconfig.Options{Executable: &executable, Arguments: []string{"original"}},
		Backend: backend,
		Runner:  func([]string, string) ([]byte, error) { calls++; return nil, nil },
	}
	clone := copyOwnedSelection(original)
	executable = "changed"
	original.Process.Arguments[0] = "changed"
	if *clone.Process.Executable != "original" || clone.Process.Arguments[0] != "original" || clone.Backend != backend {
		t.Fatalf("selection was not detached: %+v", clone)
	}
	if _, err := clone.Backend.Check(context.Background(), checkinputs.ScriptRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := clone.Runner(nil, ""); err != nil || calls != 1 {
		t.Fatalf("runner identity lost: calls=%d error=%v", calls, err)
	}
}

func TestRegisteredProviderOwnsManifestAndProcessOptions(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manifest := envelopeManifest("snapshot", []string{"bash"})
	manifest.Process.Entry = executable
	manifest.Process.Args = []string{"original-argument"}
	manifest.Process.Env = map[string]string{"SNAPSHOT_MANIFEST": "original"}
	manifest.Capabilities.Severities = map[string]manifests.Severity{"notice": manifests.SeverityWarning}
	manifest.Capabilities.Resources = []manifests.Resource{manifests.ResourceDirectory}
	options := &processconfig.Options{LauncherArguments: []string{"-test.run=^TestSnapshotProviderHelper$"}, Environment: []string{"ACTIONLINT_SNAPSHOT_HELPER=original"}}
	var retained Provider
	registration, err := RegisterProvider(Provider{Manifest: manifest, Selection: Selection{Command: "snapshot", Process: options}, Bind: func(r tools.Registration, p Provider) tools.Registration { retained = p; return r }})
	if err != nil {
		t.Fatal(err)
	}
	manifest.Process.Entry = "missing-executable"
	manifest.Process.Args[0] = "changed-argument"
	manifest.Process.Env["SNAPSHOT_MANIFEST"] = "changed"
	manifest.Capabilities.Severities["notice"] = manifests.SeverityError
	manifest.Capabilities.Resources[0] = manifests.ResourceWorkspace
	options.LauncherArguments[0] = "-test.run=^DoesNotExist$"
	options.Environment[0] = "ACTIONLINT_SNAPSHOT_HELPER=changed"
	if retained.Manifest.Process.Entry != executable || retained.Manifest.Process.Env["SNAPSHOT_MANIFEST"] != "original" || !slices.Equal(retained.Manifest.Capabilities.Resources, []manifests.Resource{manifests.ResourceDirectory}) {
		t.Fatalf("registered manifest changed: %+v", retained.Manifest)
	}
	pool := process.NewProcessPool(t.Context(), 1)
	defer pool.Wait()
	runner, err := registration.Build(tools.Context{Processes: pool})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{scriptFixture(t, "run: echo ok\n")}})
	if err != nil || len(findings) != 1 || findings[0].Severity != "warning" || findings[0].Message != "original original original-argument" {
		t.Fatalf("finding = %+v, error = %v", findings, err)
	}
}

func TestSnapshotProviderHelper(t *testing.T) {
	t.Parallel()
	value := os.Getenv("ACTIONLINT_SNAPSHOT_HELPER")
	if value == "" {
		return
	}
	argument := "missing"
	if slices.Contains(os.Args, "original-argument") {
		argument = "original-argument"
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "diagnostics": []checkinputs.ScriptDiagnostic{{Code: "SNAPSHOT", Message: value + " " + os.Getenv("SNAPSHOT_MANIFEST") + " " + argument, Severity: "notice", Location: checkinputs.ScriptPosition{Row: 1, Column: 1}}}})
	os.Exit(0)
}
