package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kjanat/wacogo"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
)

const scriptFixture03 = "testdata/plugins/script-engine-0.3.0.wasm"

func loadScriptFixture03(t *testing.T) *Plugin {
	t.Helper()
	p, err := Load(t.Context(), scriptFixture03, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return p
}

func TestScriptEngine03ContractCurrent(t *testing.T) {
	data, err := os.ReadFile("../../../crates/actionlint-extension-api/wit/0.3.0/script-engine.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != scriptEngine03ContractDigest {
		t.Fatal("stale script-engine 0.3.0 contract; regenerate")
	}
}

// The host negotiates the version a component exports: 0.3.0 engines declare
// capabilities, 0.2.0 engines keep none.
func TestScriptEngineNegotiatesExportedVersion(t *testing.T) {
	current := loadScriptFixture03(t)
	if current.scriptInterface != ScriptEngine03Interface {
		t.Fatalf("negotiated %q", current.scriptInterface)
	}
	d := current.ScriptDescriptor()
	if !d.Fixes || !d.ReportedSources || !slices.Equal(d.Resources, []analyzer.ToolResourceKind{analyzer.ToolDirectory}) || d.Rule.FixAvailability != "suggested" {
		t.Fatalf("0.3.0 capabilities = %+v", d)
	}
	legacy := loadScriptFixture(t)
	if legacy.scriptInterface != ScriptEngineInterface {
		t.Fatalf("negotiated %q", legacy.scriptInterface)
	}
	if d := legacy.ScriptDescriptor(); d.Fixes || d.ReportedSources || len(d.Resources) != 0 || d.Rule.FixAvailability != "none" {
		t.Fatalf("0.2.0 capabilities = %+v", d)
	}
}

func TestScriptEngineVersionMismatch(t *testing.T) {
	instance := func(names ...string) []wacogo.ExportDesc {
		exports := make([]wacogo.ExportDesc, len(names))
		for i, name := range names {
			exports[i] = wacogo.ExportDesc{Name: name, Kind: wacogo.SortInstance}
		}
		return exports
	}
	for _, tc := range []struct {
		name    string
		exports []wacogo.ExportDesc
		want    string
	}{
		{"unsupported version", instance("kjanat:actionlint/script-engine@0.4.0"), "this host supports " + ScriptEngineInterface + " and " + ScriptEngine03Interface},
		{"two versions", instance(ScriptEngineInterface, ScriptEngine03Interface), "export one script-engine version"},
		{"template effects 0.2.0 with script-engine 0.3.0", instance(ScriptEngine03Interface, TemplateEffectsInterface), TemplateEffectsInterface + " requires " + ScriptEngineInterface},
		{"template effects 0.3.0 with script-engine 0.2.0", instance(ScriptEngineInterface, TemplateEffects03Interface), TemplateEffects03Interface + " requires " + ScriptEngine03Interface},
		{"two template-effects versions", instance(ScriptEngine03Interface, TemplateEffectsInterface, TemplateEffects03Interface), "export one template-effects version"},
		{"unsupported template-effects version", instance(ScriptEngine03Interface, "kjanat:actionlint/template-effects@0.4.0"), "this host supports " + TemplateEffectsInterface + " and " + TemplateEffects03Interface},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := extensionRoles(tc.exports); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	for _, version := range []string{ScriptEngineInterface, ScriptEngine03Interface} {
		roles, err := extensionRoles(instance(version))
		if err != nil || roles.script != version || roles.templateEffects != "" {
			t.Fatalf("%s: roles=%+v err=%v", version, roles, err)
		}
	}
	// Each template-effects version pairs with the script engine of its package.
	for script, template := range map[string]string{ScriptEngineInterface: TemplateEffectsInterface, ScriptEngine03Interface: TemplateEffects03Interface} {
		roles, err := extensionRoles(instance(script, template))
		if err != nil || roles.script != script || roles.templateEffects != template {
			t.Fatalf("%s with %s: roles=%+v err=%v", script, template, roles, err)
		}
	}
	// A 0.3.0 component relabeled as 0.2.0 fails the structural check
	// before any guest code runs.
	data, err := os.ReadFile(scriptFixture03)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.ReplaceAll(data, []byte(ScriptEngine03Interface), []byte(ScriptEngineInterface))
	if bytes.Equal(changed, data) {
		t.Fatal("fixture does not name its interface")
	}
	path := filepath.Join(t.TempDir(), "relabeled.wasm")
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(t.Context(), path, Options{})
	if p != nil {
		_ = p.Close(t.Context())
	}
	if err == nil || !strings.Contains(err.Error(), "incompatible "+ScriptEngineInterface+" contract") {
		t.Fatalf("relabeled component: %v", err)
	}
}

// json1Request is a check whose engine decodes a ShellCheck json1 fixture.
// Its source is wide and long enough for the fixture's positions.
func json1Request(t *testing.T, name string) (analyzer.ScriptRequest, []byte) {
	t.Helper()
	stdout, err := os.ReadFile(filepath.Join("..", "..", "tools", "provider", "bundled", "shellcheck", "testdata", name, "stdout.json"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := jsonv2.Marshal(map[string]jsontext.Value{"json1": jsontext.Value(stdout)})
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Repeat(strings.Repeat("x", 40)+"\n", 8)
	return analyzer.ScriptRequest{Source: source, Filename: "actionlint.sh", Config: config}, stdout
}

// TestScriptEngine03ProcessParity decodes the same ShellCheck stdout through
// the bundled process provider and a 0.3.0 component: both routes yield the
// same diagnostics, fixes and reported sources.
func TestScriptEngine03ProcessParity(t *testing.T) {
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == "shellcheck" })
	if index < 0 {
		t.Fatal("shellcheck is not bundled")
	}
	p := loadScriptFixture03(t)
	for _, name := range []string{"script", "sourced"} {
		t.Run(name, func(t *testing.T) {
			request, stdout := json1Request(t, name)
			request.Directory = t.TempDir()
			request.DirectoryFS = directoryGrant(t, request.Directory)
			pool := analyzerhost.NewProcessPool(t.Context(), 1)
			command := pool.EmbeddedCommand("shellcheck", func([]string, string) ([]byte, error) { return stdout, nil })
			backend, err := process.NewBackend(manifests[index], command, process.Options{Programs: bundled.Program, Required: true})
			if err != nil {
				t.Fatal(err)
			}
			processed, err := backend.Run(t.Context(), analyzer.ScriptRequest{Source: request.Source, Filename: request.Filename, Directory: request.Directory}, nil)
			if err != nil {
				t.Fatal(err)
			}
			pool.Wait()
			checked, err := p.Check(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(checked) == 0 {
				t.Fatal("no diagnostics")
			}
			if diff := cmp.Diff(processed.Diagnostics, checked, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("process route and 0.3.0 component differ (-process +component):\n%s", diff)
			}
		})
	}
}

func TestScriptEngine03Excluded(t *testing.T) {
	p := loadScriptFixture03(t)
	request, _ := json1Request(t, "script")
	request.Excluded = []string{"SC2035", "SC2086"}
	diagnostics, err := p.Check(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 5 {
		t.Fatalf("got %d diagnostics, want 5", len(diagnostics))
	}
	for _, d := range diagnostics {
		if slices.Contains(request.Excluded, d.Code) {
			t.Fatalf("excluded %s was reported", d.Code)
		}
	}
	request.Excluded = []string{"SC\x002035"}
	if _, err := p.Check(t.Context(), request); err == nil {
		t.Fatal("excluded code with NUL accepted")
	}
}

// directoryGrant opens directory as the runner's grant would, closed when
// the test ends.
func directoryGrant(t *testing.T, directory string) *fsread.SnapshotFS {
	t.Helper()
	grant, err := fsread.NewSnapshot(t.Context(), directory, fsread.New(t.Context(), nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := grant.Close(); err != nil {
			t.Error(err)
		}
	})
	return grant
}

// The engine reads a file only through the host's directory grant. A
// directory path alone opens nothing.
func TestScriptEngine03DirectoryGrant(t *testing.T) {
	p := loadScriptFixture03(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "lib.sh"), []byte("echo $value\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := analyzer.ScriptRequest{Source: "echo ok\n", Filename: "actionlint.sh", Config: []byte(`{"read":"scripts/lib.sh"}`), Directory: root, DirectoryFS: directoryGrant(t, root)}
	diagnostics, err := p.Check(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := []analyzer.ScriptDiagnostic{{
		Filename: "scripts/lib.sh", Code: "READ001", Message: "echo $value", Severity: "info",
		Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 12}, Source: "scripts/lib.sh",
	}}
	if diff := cmp.Diff(want, diagnostics); diff != "" {
		t.Fatalf("diagnostics (-want +got):\n%s", diff)
	}
	grant := request.DirectoryFS
	request.DirectoryFS = nil
	if _, err := p.Check(t.Context(), request); err == nil || !strings.Contains(err.Error(), "no directory was granted") {
		t.Fatalf("check with a directory path and no grant: %v", err)
	}
	// An engine that did not request the resource sees no directory either.
	p.scriptDescriptor.Resources = nil
	request.DirectoryFS = grant
	if _, err := p.Check(t.Context(), request); err == nil || !strings.Contains(err.Error(), "no directory was granted") {
		t.Fatalf("check without the resource: %v", err)
	}
}

func TestScriptEngine03RejectsUndeclaredCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, fixture, want string
		directory           bool
		descriptor          func(*analyzer.ScriptEngineDescriptor)
	}{
		{"fixes", "script", "does not declare them", false, func(d *analyzer.ScriptEngineDescriptor) { d.Fixes = false }},
		{"reported sources", "sourced", "does not declare reported sources", true, func(d *analyzer.ScriptEngineDescriptor) { d.ReportedSources = false }},
		{"source without a grant", "sourced", "without a granted directory", false, func(*analyzer.ScriptEngineDescriptor) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := loadScriptFixture03(t)
			tc.descriptor(p.scriptDescriptor)
			request, _ := json1Request(t, tc.fixture)
			if tc.directory {
				request.Directory = t.TempDir()
				request.DirectoryFS = directoryGrant(t, request.Directory)
			}
			if _, err := p.Check(t.Context(), request); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

const templateFixture03 = "testdata/plugins/template-script-engine-0.3.0.wasm"

func TestTemplateEffects03ContractCurrent(t *testing.T) {
	data, err := os.ReadFile("../../../crates/actionlint-extension-api/wit/0.3.0/template-effects.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != templateEffects03ContractDigest {
		t.Fatal("stale template-effects 0.3.0 contract; regenerate")
	}
}

// A template-aware 0.3.0 engine keeps the 0.3.0 capabilities: its
// template-effects check carries excluded codes and the directory grant, and
// its findings carry fixes and reported sources besides their effects.
func TestTemplateEffects03Check(t *testing.T) {
	p, err := Load(t.Context(), templateFixture03, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if p.scriptInterface != ScriptEngine03Interface || p.templateInterface != TemplateEffects03Interface {
		t.Fatalf("negotiated %q and %q", p.scriptInterface, p.templateInterface)
	}
	d := p.ScriptDescriptor()
	if d.TemplateContract != analyzer.TemplateEffectsContract || !d.Fixes || !d.ReportedSources || !slices.Equal(d.Resources, []analyzer.ToolResourceKind{analyzer.ToolDirectory}) {
		t.Fatalf("descriptor = %+v", d)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("echo lib\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := analyzer.ScriptRequest{Source: "x\n", Filename: "actionlint.sh", Config: []byte(`{"read":"lib.sh"}`), Directory: root, DirectoryFS: directoryGrant(t, root)}
	diagnostics, err := p.Check(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := []analyzer.ScriptDiagnostic{
		{
			Filename: "actionlint.sh", Code: "TPL001", Message: "first character", Severity: "style", TemplateEffect: analyzer.TemplateEffectLineLength,
			Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 2},
			Fixes: []analyzer.ScriptFix{{Description: "replace it", Replacements: []analyzer.ScriptReplacement{{Row: 1, Column: 1, EndRow: 1, EndColumn: 2, Text: "y"}}}},
		},
		{
			Filename: "lib.sh", Code: "READ001", Message: "echo lib", Severity: "info", Source: "lib.sh", TemplateEffect: analyzer.TemplateEffectNone,
			Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 9},
		},
	}
	if diff := cmp.Diff(want, diagnostics); diff != "" {
		t.Fatalf("diagnostics (-want +got):\n%s", diff)
	}
	request.Excluded, request.Config = []string{"TPL001"}, nil
	if diagnostics, err := p.Check(t.Context(), request); err != nil || len(diagnostics) != 0 {
		t.Fatalf("excluded: %+v %v", diagnostics, err)
	}
}
