package execution

import (
	"testing"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

func TestResolveShellPreservesSemanticStates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		step     value.Value
		job      value.Value
		want     string
		implicit bool
	}{
		{name: "missing uses implicit runner bash", want: "bash", implicit: true},
		{name: "dynamic step stops fallback", step: value.DynamicValue("${{ inputs.shell }}", value.String, value.Origin{}), job: value.YAMLScalar("pwsh", "!!str", value.Origin{})},
		{name: "evaluated expression text stays data", step: value.FromJSON("${{ github.ref }}", value.Origin{}), job: value.YAMLScalar("pwsh", "!!str", value.Origin{}), want: "${{ github.ref }}"},
		{name: "custom shell keeps literal executable", step: value.Template("bash --noprofile ${{ inputs.flags }} {0}", value.Origin{}), want: "bash --noprofile ${{ inputs.flags }} {0}"},
		{name: "invalid step stops fallback", step: value.Condition("${{ true + }}", value.Origin{}), job: value.YAMLScalar("pwsh", "!!str", value.Origin{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveShell(tc.step, tc.job, value.Value{}, value.Value{})
			if got.Name != tc.want || got.Implicit != tc.implicit {
				t.Fatalf("ResolveShell() = %+v, want name=%q implicit=%v", got, tc.want, tc.implicit)
			}
		})
	}
}

func TestDefaultShellUsesCanonicalSemanticValues(t *testing.T) {
	t.Parallel()
	defaults := value.Template(`${{ fromJSON('{"shell":"pwsh"}') }}`, value.Origin{})
	if got := DefaultShell(defaults); !got.Known() || got.Kind() != value.String {
		t.Fatalf("folded defaults shell = state %v kind %v", got.State(), got.Kind())
	} else if shell, _ := got.Text(); shell != "pwsh" {
		t.Fatalf("folded defaults shell = %q", shell)
	}
}

func TestDirectoryValueDoesNotRetemplateEvaluatedText(t *testing.T) {
	t.Parallel()
	text := "${{ github.workspace }}"
	got := DirectoryValue(value.FromJSON(text, value.Origin{}))
	if got.Kind != DirectoryKnown || got.Path != text {
		t.Fatalf("evaluated directory text reinterpreted: %+v", got)
	}
	paths := Paths{}
	got = paths.WorkingDirectory(value.DynamicValue("${{ github.workspace }}/src", value.String, value.Origin{}))
	if got.Kind != DirectoryKnown || got.Path != "./src" {
		t.Fatalf("dynamic workspace directory = %+v", got)
	}
}
