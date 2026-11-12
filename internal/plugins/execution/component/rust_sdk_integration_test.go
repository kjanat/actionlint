package component

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
)

// TestRustSDKScriptWorlds builds the downstream fixture from the SDK's Cargo
// metadata and loads each package through the same path used for real plugins.
// It is opt-in because it builds Rust components and requires wasm32-wasip2.
func TestRustSDKScriptWorlds(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_RUST_SDK_INTEGRATION") != "1" {
		t.Skip("set ACTIONLINT_RUST_SDK_INTEGRATION=1 to build the Rust SDK fixture")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "plugins", "sdk", "rust", "tests", "wit-consumer")
	target := filepath.Join(root, "target", "wit-consumer")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "cargo", args...)
		cmd.Dir = fixture
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cargo %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	for _, tc := range []struct {
		name, features, iface string
		template              bool
	}{
		{"script-02", "", ScriptEngineInterface, false},
		{"template-02", "template", ScriptEngineInterface, true},
		{"script-03", "v03", ScriptEngine03Interface, false},
		{"template-03", "v03,template", ScriptEngine03Interface, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			featureArgs := []string{}
			if tc.features != "" {
				featureArgs = []string{"--features", tc.features}
			}
			build := append([]string{"build", "--locked", "--target", "wasm32-wasip2", "--target-dir", target, "--lib", "-j", "1"}, featureArgs...)
			run(build...)
			input := filepath.Join(target, "wasm32-wasip2", "debug", "wit_consumer_test.wasm")
			output := filepath.Join(t.TempDir(), "distribution")
			packageArgs := append([]string{"run", "--locked", "--target-dir", target, "--bin", "package", "-j", "1"}, featureArgs...)
			packageArgs = append(packageArgs, "--", input, output)
			run(packageArgs...)
			p, err := Load(t.Context(), filepath.Join(output, "plugin.wasm"), Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := p.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			if p.scriptInterface != tc.iface {
				t.Fatalf("script interface = %q, want %q", p.scriptInterface, tc.iface)
			}
			desc := p.ScriptDescriptor()
			if desc.ID != "example" || desc.Engine != "example" {
				t.Fatalf("descriptor = %#v", desc)
			}
			if (desc.TemplateContract != "") != tc.template {
				t.Fatalf("template contract = %q", desc.TemplateContract)
			}
			if err := p.ValidateConfig(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			findings, err := p.Check(t.Context(), checkinputs.ScriptRequest{Source: "pass", Filename: "example.py"})
			if err != nil || len(findings) != 0 {
				t.Fatalf("check = %#v, %v", findings, err)
			}
		})
	}
}
