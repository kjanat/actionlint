package component

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
)

func TestReadInterfaces(t *testing.T) {
	t.Parallel()
	data := wasmcomponent.Build([]string{"wasi:cli/environment@0.2.0"}, []string{AnalyzerInterface}, []string{"plain-type"})
	interfaces, err := ReadInterfaces(data)
	if err != nil || !slices.Equal(interfaces.Imports, []string{"wasi:cli/environment@0.2.0"}) || !slices.Equal(interfaces.Exports, []string{AnalyzerInterface}) {
		t.Fatalf("interfaces=%+v error=%v", interfaces, err)
	}
	if err := CheckInterfaces(interfaces); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInterfaces(append(data, 0xff)); err == nil || !strings.Contains(err.Error(), "validate component contract") {
		t.Fatalf("truncated component: %v", err)
	}
}

func TestCheckInterfacesImports(t *testing.T) {
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
		err := CheckInterfaces(tc.interfaces)
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
