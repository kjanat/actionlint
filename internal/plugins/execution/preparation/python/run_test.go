package python

import (
	"testing"
)

func TestRunSelectsPythonShells(t *testing.T) {
	for _, tc := range []struct {
		shell        *string
		skip, target string
	}{
		{shell: nil, skip: "its step shell does not run Python"},
		{shell: new("bash"), skip: "its step shell does not run Python"},
		{shell: new("${{ matrix.shell }}"), skip: "its step shell does not run Python"},
		{shell: new("python")},
		{shell: new("python3.12 {0}"), target: "py312"},
	} {
		prepared, err := Run("print(1)", tc.shell)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Skip != tc.skip || prepared.Vars[VarTargetVersion] != tc.target {
			t.Fatalf("shell %v: got skip %q, vars %q", tc.shell, prepared.Skip, prepared.Vars)
		}
	}
}

func TestEngineConfigAddsTheShellTarget(t *testing.T) {
	for _, tc := range []struct{ config, target, want string }{
		{config: "", want: `{}`},
		{config: "null", target: "py312", want: `{"target-version":"py312"}`},
		{config: `{"lint":{"select":["F"]}}`, target: "py312", want: `{"lint":{"select":["F"]},"target-version":"py312"}`},
		{config: `{"target-version":"py38"}`, target: "py312", want: `{"target-version":"py38"}`},
	} {
		vars := map[string]string{}
		if tc.target != "" {
			vars[VarTargetVersion] = tc.target
		}
		got, err := EngineConfig([]byte(tc.config), vars)
		if err != nil || string(got) != tc.want {
			t.Fatalf("%s with %q: got %s, %v; want %s", tc.config, tc.target, got, err, tc.want)
		}
	}
}
