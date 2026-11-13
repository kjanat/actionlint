package scripts_test

import (
	"testing"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestDefaultShell(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, runner, want string
		container          value.Value
		known              bool
	}{
		{name: "Linux runner", runner: "bash", want: "bash", known: true},
		{name: "Windows runner", runner: "pwsh", want: "pwsh", known: true},
		{name: "unknown runner"},
		{name: "container overrides runner", runner: "bash", container: value.YAMLScalar("alpine", "!!str", value.Origin{}), want: "sh", known: true},
		{name: "container with unknown runner", container: value.YAMLScalar("alpine", "!!str", value.Origin{}), want: "sh", known: true},
		{name: "empty container uses runner", runner: "pwsh", container: value.YAMLScalar("", "!!str", value.Origin{}), want: "pwsh", known: true},
		{name: "unknown container blocks runner fallback", runner: "bash", container: value.DynamicValue("${{ inputs.container }}", value.KindUnknown, value.Origin{})},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fallback := scriptmodel.DefaultShell(test.container, test.runner)
			text, known := fallback.ScalarString()
			if text != test.want || known != test.known {
				t.Fatalf("default shell = %q, known=%t; want %q, known=%t", text, known, test.want, test.known)
			}
			resolved := execution.ResolveShell(value.Value{}, value.Value{}, value.Value{}, fallback)
			if resolved.Name != test.want || resolved.Implicit != test.known {
				t.Fatalf("resolved default = %+v", resolved)
			}
			explicit := execution.ResolveShell(value.YAMLScalar("custom {0}", "!!str", value.Origin{}), value.Value{}, value.Value{}, fallback)
			if explicit.Name != "custom {0}" || explicit.Implicit {
				t.Fatalf("explicit shell did not override platform default: %+v", explicit)
			}
		})
	}
}

func TestContainerShellUsesSemanticValues(t *testing.T) {
	t.Parallel()
	container := value.Map(value.Origin{}, value.Prop{Name: "image", Value: value.YAMLScalar("node:22", "!!str", value.Origin{})})
	if got := scriptmodel.ContainerShell(container); !got.Known() {
		t.Fatalf("container shell state = %v", got.State())
	} else if shell, _ := got.Text(); shell != "sh" {
		t.Fatalf("container shell = %q", shell)
	}
	if got := scriptmodel.ContainerShell(value.DynamicValue("${{ inputs.container }}", value.Object, value.Origin{})); got.State() != value.Dynamic {
		t.Fatalf("dynamic container shell state = %v", got.State())
	}
}

func TestContainerScalarImages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		state        value.State
	}{
		{"scalar image", "${{ 'node:22' }}", value.Literal},
		{"empty scalar", "${{ '' }}", value.Missing},
		{"unknown scalar", "${{ inputs.image }}", value.Dynamic},
		{"invalid shape", "${{ fromJSON('[]') }}", value.Dynamic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := scriptmodel.ContainerShell(value.Template(tc.source, value.Origin{}))
			if got.State() != tc.state {
				t.Fatalf("state=%v, want %v", got.State(), tc.state)
			}
			if got.Known() {
				if text, _ := got.Text(); text != "sh" {
					t.Fatalf("container shell=%q", text)
				}
			}
		})
	}
}
