package component

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestReadInterfaces(t *testing.T) {
	t.Parallel()
	data := wasmcomponent.Build([]string{"wasi:cli/environment@0.2.0"}, []string{AnalyzerInterface}, []string{"plain-type"})
	interfaces, err := ReadInterfaces(data)
	if err != nil || !slices.Equal(interfaces.Imports, []string{"wasi:cli/environment@0.2.0"}) || !slices.Equal(interfaces.Exports, []string{AnalyzerInterface}) {
		t.Fatalf("interfaces=%+v error=%v", interfaces, err)
	}
	if _, err := Admit(ListedInterfaces{Interfaces: interfaces}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInterfaces(append(data, 0xff)); err == nil || !strings.Contains(err.Error(), "validate component contract") {
		t.Fatalf("truncated component: %v", err)
	}
}

func TestAdmitDecidesImports(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		interfaces Interfaces
		want       string
	}{
		{Interfaces{Imports: []string{HostInterface, "wasi:io/streams@0.2.0"}, Exports: []string{AnalyzerInterface}}, ""},
		{Interfaces{Imports: []string{"kjanat:actionlint/host@9.0.0"}, Exports: []string{AnalyzerInterface}}, "plugin imports kjanat:actionlint/host@9.0.0; this host provides " + HostInterface},
		{Interfaces{Imports: []string{RepositoryObjectsInterface}, Exports: []string{AnalyzerInterface}}, "repository objects require the reference-resolver role"},
		{Interfaces{Imports: []string{RepositoryObjectsInterface}, Exports: []string{ReferenceResolverInterface}}, ""},
	} {
		_, err := Admit(ListedInterfaces{Interfaces: tc.interfaces})
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || err.Error() != tc.want) {
			t.Errorf("%+v: error=%v, want %q", tc.interfaces, err, tc.want)
		}
	}
}

func TestAcceptedInterfacesDigest(t *testing.T) {
	t.Parallel()
	lines := acceptedInterfaces()
	for _, want := range []string{"export " + AnalyzerInterface, "export " + Runner03Interface, "export " + TemplateEffects03Interface, "import " + HostInterface, "import " + RepositoryObjectsInterface, "import wasi:cli/"} {
		if !slices.Contains(lines, want) {
			t.Errorf("accepted interfaces omit %q", want)
		}
	}
	if !slices.IsSorted(lines) || AcceptedInterfacesDigest() != sha256hex.Digest([]byte(strings.Join(lines, "\n"))) {
		t.Fatalf("digest %s does not cover sorted %v", AcceptedInterfacesDigest(), lines)
	}
}

func TestAdmitReturnsExactRoles(t *testing.T) {
	t.Parallel()
	all := []string{AnalyzerInterface, ToolIntegrationInterface, ReferenceResolverInterface, RunnerInterface, ScriptEngine03Interface, TemplateEffects03Interface}
	roles, err := Admit(ListedInterfaces{Interfaces: Interfaces{Exports: all}})
	want := Roles{Analyzer: AnalyzerInterface, ToolIntegration: ToolIntegrationInterface, ReferenceResolver: ReferenceResolverInterface, Runner: RunnerInterface, ScriptEngine: ScriptEngine03Interface, TemplateEffects: TemplateEffects03Interface}
	if err != nil || roles != want || !slices.Equal(roles.Exports(), all) {
		t.Fatalf("roles=%+v error=%v", roles, err)
	}
	for _, runner := range []string{RunnerInterface, Runner03Interface} {
		if roles, err := Admit(ListedInterfaces{Interfaces: Interfaces{Exports: []string{runner}}}); err != nil || roles != (Roles{Runner: runner}) {
			t.Fatalf("%s: roles=%+v error=%v", runner, roles, err)
		}
	}
	if roles, err := Admit(ListedInterfaces{Interfaces: Interfaces{Exports: []string{"kjanat:actionlint/runner-provider@0.4.0"}}}); err == nil || roles != (Roles{}) {
		t.Fatalf("unsupported runner: roles=%+v error=%v", roles, err)
	}
}

func TestAppendItemsReportsSectionErrors(t *testing.T) {
	t.Parallel()
	broken := errors.New("broken item")
	items, err := appendItems([]string{"kept"}, func(yield func(string, error) bool) {
		if yield("read", nil) {
			yield("", broken)
		}
	})
	if !errors.Is(err, broken) || !slices.Equal(items, []string{"kept", "read"}) {
		t.Fatalf("items=%v error=%v", items, err)
	}
}

func TestCheckDeclaredInterface(t *testing.T) {
	t.Parallel()
	roles := Roles{Runner: Runner03Interface}
	for _, tc := range []struct {
		metadata, want string
	}{
		{"", ""},
		{`{"id":"example"}`, ""},
		{`{"interface":"` + Runner03Interface + `"}`, ""},
		{`{"interface":5}`, "plugin metadata: "},
		{`{"interface":"` + RunnerInterface + `"}`, "plugin metadata declares " + RunnerInterface + "; the component exports " + Runner03Interface},
	} {
		err := checkDeclaredInterface([]byte(tc.metadata), roles)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.want)) {
			t.Errorf("%s: error=%v, want %q", tc.metadata, err, tc.want)
		}
	}
}

func withMetadata(data []byte, metadata string) []byte {
	section := binary.AppendUvarint(nil, uint64(len(artifacts.MetadataSection)))
	section = append(append(section, artifacts.MetadataSection...), metadata...)
	data = append(slices.Clone(data), 0)
	return append(binary.AppendUvarint(data, uint64(len(section))), section...)
}

const olderRunnerMetadata = `{"metadata_version":1,"id":"example","transport":"wasm-component","interface":"` + RunnerInterface + `"}`

func TestLoadRejectsMetadataForAnotherInterfaceVersion(t *testing.T) {
	t.Parallel()
	data := withMetadata(wasmcomponent.Build(nil, []string{Runner03Interface}, nil), olderRunnerMetadata)
	path := filepath.Join(t.TempDir(), "runner.wasm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	plugin, err := LoadComponentPlugin(t.Context(), path, Options{})
	if err == nil {
		_ = plugin.Close(t.Context())
	}
	want := "plugin metadata declares " + RunnerInterface + "; the component exports " + Runner03Interface
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%v, want %q", err, want)
	}
	if _, err := Admit(ListedInterfaces{Interfaces: Interfaces{Exports: []string{Runner03Interface}}, Metadata: []byte(olderRunnerMetadata)}); err == nil || err.Error() != want {
		t.Fatalf("listed interfaces: error=%v, want %q", err, want)
	}
}

func TestAdmitMatchesLoad(t *testing.T) {
	t.Parallel()
	loadable, err := os.ReadFile(filepath.Join("testdata", "plugins", "no-sudo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"loadable":              loadable,
		"analyzer name only":    wasmcomponent.Build(nil, []string{AnalyzerInterface}, nil),
		"core module":           {0, 'a', 's', 'm', 1, 0, 0, 0},
		"older runner metadata": withMetadata(wasmcomponent.Build(nil, []string{Runner03Interface}, nil), olderRunnerMetadata),
		"not WebAssembly":       []byte("plugin"),
	} {
		path := filepath.Join(t.TempDir(), "plugin.wasm")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		roles, checked := Admit(ComponentBytes(data))
		plugin, loaded := LoadComponentPlugin(t.Context(), path, Options{})
		if loaded == nil {
			if err := plugin.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		switch {
		case checked == nil && loaded == nil:
			if roles != (Roles{Analyzer: AnalyzerInterface}) || plugin.Roles() != roles {
				t.Errorf("%s: selection roles %+v, load roles %+v", name, roles, plugin.Roles())
			}
		case checked == nil || loaded == nil || !strings.Contains(loaded.Error(), stableReason(checked)):
			t.Errorf("%s: selection error %v, load error %v", name, checked, loaded)
		}
	}
}

// stableReason drops the missing export name, which the subtype checker
// picks from a map.
func stableReason(err error) string {
	reason, _, _ := strings.Cut(err.Error(), "`")
	return reason
}
