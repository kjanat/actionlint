package shellcheck_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/shellcheck"
)

type documentGuest struct {
	inputs []analyzer.Input
	fail   bool
}

type sourceProbeGuest struct {
	documentGuest
	directory, file string
}

func (g *sourceProbeGuest) NegotiateTool(_ context.Context, data, _ json.RawMessage) (*analyzer.ToolNegotiation, error) {
	return &analyzer.ToolNegotiation{Options: data, Resources: []analyzer.ToolResourceRequest{{Kind: analyzer.ToolWorkspace}, {Kind: analyzer.ToolDirectory, Path: g.directory}}}, nil
}

func (g *sourceProbeGuest) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	g.inputs = append(g.inputs, input)
	content, err := fs.ReadFile(input.Files[len(input.Files)-1].FS, g.file)
	if err != nil {
		return nil, err
	}
	return []diagnostic.Diagnostic{{Message: string(content), Path: input.Path, Start: input.Scripts[0].Start}}, nil
}

func TestDocumentPluginCannotGrantRepositoryPathsOutsideWorkspace(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("private-host-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	filesystemRoot := filepath.VolumeName(outside) + string(os.PathSeparator)
	rootFile, err := filepath.Rel(filesystemRoot, secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, directory, file string }{
		{"absolute directory", outside, "secret.txt"},
		{"filesystem root", filesystemRoot, filepath.ToSlash(rootFile)},
		{"configured symlink", alias, "secret.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guest := &sourceProbeGuest{directory: tc.directory, file: tc.file}
			registration, err := (shellcheck.Options{Command: "shellcheck"}).Registration().WithAnalyzer(guest)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(workspace, "actionlint.yaml")
			if err := os.WriteFile(configPath, []byte("tools: {shellcheck: {config: {external-sources: true, source-path: ["+strconv.Quote(tc.directory)+"]}}}"), 0o600); err != nil {
				t.Fatal(err)
			}
			options := application.Options{WorkingDir: workspace, ConfigFile: configPath, Tools: []tools.Registration{registration}}
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n")})
			if err == nil || len(guest.inputs) != 0 {
				var messages []string
				if result != nil {
					for _, finding := range result.Diagnostics {
						messages = append(messages, finding.Message)
					}
				}
				t.Fatalf("outside source access reached guest: inputs=%d messages=%v error=%v", len(guest.inputs), messages, err)
			}
			if !strings.Contains(err.Error(), "workspace root") {
				t.Fatalf("wrong rejection: %v", err)
			}
		})
	}
}

func TestDocumentPluginReadsWorkspaceSources(t *testing.T) {
	workspace := t.TempDir()
	sources := filepath.Join(workspace, "sources")
	if err := os.Mkdir(sources, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sources, "value.txt"), []byte("workspace-source-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "alias")
	if err := os.Symlink(sources, alias); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"sources", alias} {
		t.Run(directory, func(t *testing.T) {
			guest := &sourceProbeGuest{directory: directory, file: "value.txt"}
			registration, err := (shellcheck.Options{Command: "shellcheck"}).Registration().WithAnalyzer(guest)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(workspace, "actionlint.yaml")
			if err := os.WriteFile(configPath, []byte("tools: {shellcheck: {config: {source-path: ["+strconv.Quote(directory)+"]}}}"), 0o600); err != nil {
				t.Fatal(err)
			}
			options := application.Options{WorkingDir: workspace, ConfigFile: configPath, Tools: []tools.Registration{registration}}
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n")})
			if err != nil {
				t.Fatal(err)
			}
			if len(guest.inputs) != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "workspace-source-marker" {
				t.Fatalf("workspace source unavailable: inputs=%d diagnostics=%+v", len(guest.inputs), result.Diagnostics)
			}
		})
	}
}

func (*documentGuest) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "shellcheck", Recommended: true, Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Execution}}
}

func (*documentGuest) ToolIntegration() *analyzer.ToolIntegration {
	return &analyzer.ToolIntegration{Tool: "shellcheck", Contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
}

func (*documentGuest) NegotiateTool(_ context.Context, data, _ json.RawMessage) (*analyzer.ToolNegotiation, error) {
	var config struct {
		RCFile  string   `json:"rc-file"`
		Disable []string `json:"disable"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.RCFile != "" {
		return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "fixture declines rc files"}
	}
	for _, code := range config.Disable {
		if strings.Contains(code, "-") {
			return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "fixture declines ranges"}
		}
	}
	return &analyzer.ToolNegotiation{Options: data}, nil
}

func (g *documentGuest) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	g.inputs = append(g.inputs, input)
	start, _ := input.Scripts[0].Position(1, 6, false)
	end, _ := input.Scripts[0].Position(1, 12, true)
	reference := input.Scripts[0].Reference
	result := []diagnostic.Diagnostic{{
		Reference: &reference, Rule: "shellcheck", Code: "SC2086", Message: "Quote variable", Path: input.Path, Start: start, End: end,
		Fixes: []diagnostic.Fix{{
			Description: "Quote", Applicability: "unsafe", Revisions: map[string]string{input.Path: input.Reference.Revision},
			Edits: []diagnostic.Edit{{Path: input.Path, Start: start, End: end, Replacement: `"$VALUE"`}},
		}},
	}}
	if g.fail {
		return result, errors.New("partial analysis")
	}
	return result, nil
}

func TestDocumentPluginRetainsShellcheckControlsAndFixes(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		disabled     bool
		partial      bool
		process      *processconfig.Options
	}{
		{"enabled", "{}", false, false, nil},
		{"tool disabled", "tools: {shellcheck: false}", true, false, nil},
		{"rule disabled", "lint: {rules: {external: {shellcheck: off}}}", true, false, nil},
		{"inline options", "tools: {shellcheck: {config: {disable: [SC2001], enable: [quote-safe-variables], shell: bash, extended-analysis: false}}}", false, false, nil},
		{"override disabled", "overrides: [{includes: ['ci.yml'], tools: {shellcheck: false}}]", true, false, nil},
		{"partial findings", "{}", false, true, nil},
		{"working directory", "{}", false, false, &processconfig.Options{Optional: true, WorkingDir: t.TempDir()}},
		{"wasm permitted", "{}", false, false, &processconfig.Options{Optional: true, AllowWasm: true, WorkingDir: t.TempDir()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guest := &documentGuest{fail: tc.partial}
			registration, err := (shellcheck.Options{Command: "shellcheck", Process: tc.process}).Registration().WithAnalyzer(guest)
			if err != nil {
				t.Fatal(err)
			}
			overlay, err := config.ParseOverlay("config", []byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			options := application.Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{overlay}, Tools: []tools.Registration{registration}}
			needed, err := application.RequiredTools(t.Context(), options, nil)
			if err != nil || needed["shellcheck"] {
				t.Fatalf("native dependency: %v %v", needed, err)
			}
			const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE # shellcheck disable=SC2034
`
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)})
			if (err != nil) != tc.partial {
				t.Fatal(err)
			}
			if tc.disabled {
				if len(guest.inputs) != 0 || len(result.Diagnostics) != 0 {
					t.Fatalf("disabled plugin ran: %d %+v", len(guest.inputs), result.Diagnostics)
				}
				return
			}
			if len(guest.inputs) != 1 || len(result.Diagnostics) != 1 {
				t.Fatalf("duplicate/missing findings: %d %+v", len(guest.inputs), result.Diagnostics)
			}
			comments := guest.inputs[0].Scripts[0].Comments
			if len(comments) != 1 || comments[0].Text != "# shellcheck disable=SC2034" || comments[0].Placement != analyzer.TrailingComment || comments[0].Start != (diagnostic.Position{Line: 6, Column: 26}) {
				t.Fatalf("document plugin lost attached YAML comments: %+v", comments)
			}
			finding := result.Diagnostics[0]
			if finding.Rule != "shellcheck" || finding.Start.Line != 6 || finding.Start.Column != 19 || len(finding.Fixes) != 1 || finding.Fixes[0].Revisions[finding.Path] != sourceid.Digest([]byte(workflow)) {
				t.Fatalf("mapping/fix changed: %+v", finding)
			}
			if finding.Reference == nil || finding.Reference.Document == "" || finding.Reference.Node == "" || finding.Reference.Address == "$" {
				t.Fatalf("missing script identity: %+v", finding.Reference)
			}
			if tc.name == "inline options" {
				var options struct {
					Disable  []string `json:"disable"`
					Enable   []string `json:"enable"`
					Shell    string   `json:"shell"`
					Extended bool     `json:"extended-analysis"`
				}
				if err := json.Unmarshal(guest.inputs[0].Options, &options); err != nil || !slices.Equal(options.Disable, []string{"SC2001"}) || options.Shell != "bash" || options.Extended || !slices.Equal(options.Enable, []string{"quote-safe-variables"}) {
					t.Fatalf("translated options: %s %v", guest.inputs[0].Options, err)
				}
			}
		})
	}
}

func TestDocumentPluginPreservesExplicitNativeSettings(t *testing.T) {
	for _, text := range []string{
		"tools: {shellcheck: {config: .shellcheckrc}}",
		"tools: {shellcheck: {config: {disable: [SC1000-SC2000]}}}",
		"tools: {shellcheck: {backend: {process: {executable: shellcheck-custom}}}}",
	} {
		t.Run(text, func(t *testing.T) {
			overlay, err := config.ParseOverlay("config", []byte(text))
			if err != nil {
				t.Fatal(err)
			}
			registration, err := (shellcheck.Options{Command: "shellcheck"}).Registration().WithAnalyzer(&documentGuest{})
			if err != nil {
				t.Fatal(err)
			}
			needed, err := application.RequiredTools(t.Context(), application.Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{overlay}, Tools: []tools.Registration{registration}}, nil)
			if err != nil || !needed["shellcheck"] {
				t.Fatalf("explicit native settings lost: %v %v", needed, err)
			}
		})
	}
	for _, options := range []shellcheck.Options{
		{Command: "custom-shellcheck"},
		{Command: "shellcheck", Process: &processconfig.Options{Arguments: []string{"--norc"}}},
	} {
		registration, err := options.Registration().WithAnalyzer(&documentGuest{})
		if err != nil {
			t.Fatal(err)
		}
		needed, err := registration.NeedsProcess(t.Context(), nil, nil, false)
		if err != nil || !needed {
			t.Fatalf("explicit invocation lost: %t %v", needed, err)
		}
	}
}

func TestIntegratedRuleLevelNativeFallbackPlan(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		name, rules, extra string
		native             bool
	}{
		{"plugin enables legacy rule", "external: {shellcheck: off}, plugins: {shellcheck: on}", "", true},
		{"plugin disables legacy rule", "external: {shellcheck: on}, plugins: {shellcheck: off}", "", false},
		{"global lint disabled", "external: {shellcheck: off}, plugins: {shellcheck: on}", "lint: {enabled: false}", false},
		{"global rule disabled", "disable: [shellcheck], external: {shellcheck: off}, plugins: {shellcheck: on}", "", false},
		{"tool disabled", "external: {shellcheck: off}, plugins: {shellcheck: on}", "tools: {shellcheck: false}", false},
		{"override enables plugin", "external: {shellcheck: off}, plugins: {shellcheck: off}", "overrides: [{includes: [ci.yml], lint: {rules: {plugins: {shellcheck: on}}}}]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, ".shellcheckrc"), []byte("shell=bash\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			base, err := config.ParseOverlay("config", []byte("tools: {shellcheck: {config: .shellcheckrc}}\nlint: {rules: {"+tc.rules+"}}"), "shellcheck")
			if err != nil {
				t.Fatal(err)
			}
			overlays := []config.Overlay{base}
			if tc.extra != "" {
				extra, err := config.ParseOverlay("config", []byte(tc.extra), "shellcheck")
				if err != nil {
					t.Fatal(err)
				}
				overlays = append(overlays, extra)
			}
			guest := &documentGuest{}
			options := application.Options{
				WorkingDir: root, SkipProjectConfig: true, ConfigOverlays: overlays,
				Tools:     []tools.Registration{(shellcheck.Options{Command: "shellcheck"}).Registration()},
				Analyzers: []analyzer.Registration{{Analyzer: guest}},
			}
			needed, err := application.RequiredTools(t.Context(), options, []string{"ci.yml"})
			if err != nil || needed["shellcheck"] != tc.native {
				t.Fatalf("native fallback plan: %v, %v; want %t", needed, err, tc.native)
			}
			const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			_, err = application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)})
			if tc.native {
				if !errors.Is(err, exec.ErrNotFound) {
					t.Fatalf("native fallback did not require the missing executable: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(guest.inputs) != 0 {
				t.Fatal("guest analyzed a disabled rule or unsupported configuration")
			}
		})
	}
}

func TestManagedOnlyDisabledPluginDoesNotRequireFallback(t *testing.T) {
	for _, setting := range []string{
		"lint: {enabled: false}",
		"lint: {rules: {disable: [shellcheck]}}",
		"lint: {rules: {external: {shellcheck: off}}}",
		"tools: {shellcheck: {enabled: false}}",
		"overrides: [{includes: [ci.yml], tools: {shellcheck: false}}]",
	} {
		t.Run(setting, func(t *testing.T) {
			base, err := config.ParseOverlay("config", []byte("tools: {shellcheck: {config: .shellcheckrc}}"))
			if err != nil {
				t.Fatal(err)
			}
			disabled, err := config.ParseOverlay("config", []byte(setting))
			if err != nil {
				t.Fatal(err)
			}
			guest := &documentGuest{}
			options := application.Options{
				WorkingDir: t.TempDir(), SkipProjectConfig: true, ConfigOverlays: []config.Overlay{base, disabled},
				Tools:     []tools.Registration{(shellcheck.Options{Command: "shellcheck", ManagedOnly: true}).Registration()},
				Analyzers: []analyzer.Registration{{Analyzer: guest}},
			}
			needed, err := application.RequiredTools(t.Context(), options, []string{"ci.yml"})
			if err != nil || needed["shellcheck"] {
				t.Fatalf("disabled managed plugin requested fallback: %v, %v", needed, err)
			}
			const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			if _, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)}); err != nil {
				t.Fatal(err)
			}
			if len(guest.inputs) != 0 {
				t.Fatal("disabled managed plugin analyzed the document")
			}
		})
	}
}

func TestDocumentPluginPlansCompositeNativeFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	for _, directory := range []string{".git", ".github/workflows", "nested"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
	for name, content := range map[string]string{
		workflow: "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/nested\n",
		filepath.Join(root, "nested", "action.yml"): "name: Nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n",
	} {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	overlay, err := config.ParseOverlay("config", []byte("overrides: [{includes: ['nested/**'], tools: {shellcheck: {config: .shellcheckrc}}}]"))
	if err != nil {
		t.Fatal(err)
	}
	guest := &documentGuest{}
	options := application.Options{
		WorkingDir: root, ConfigOverlays: []config.Overlay{overlay},
		Tools:     []tools.Registration{(shellcheck.Options{Command: "shellcheck"}).Registration()},
		Analyzers: []analyzer.Registration{{Analyzer: guest}},
	}
	needed, err := application.RequiredTools(t.Context(), options, []string{workflow})
	if err != nil || !needed["shellcheck"] {
		t.Errorf("composite native fallback was omitted: %v, %v", needed, err)
	}
	_, err = application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("composite fallback did not require the missing executable: %v", err)
	}
	if len(guest.inputs) != 0 {
		t.Fatal("guest analyzed the unsupported composite configuration")
	}
}

type planningContextGuest struct {
	documentGuest
	contexts chan context.Context
	release  <-chan struct{}
}

func (g *planningContextGuest) NegotiateTool(ctx context.Context, data, _ json.RawMessage) (*analyzer.ToolNegotiation, error) {
	g.contexts <- ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.release:
		return &analyzer.ToolNegotiation{Options: data}, nil
	}
}

func TestDocumentPluginPlanningPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release, finished := make(chan struct{}), make(chan struct{})
	guest := &planningContextGuest{contexts: make(chan context.Context, 1), release: release}
	options := application.Options{
		WorkingDir: t.TempDir(), SkipProjectConfig: true,
		Tools:     []tools.Registration{(shellcheck.Options{Command: "shellcheck"}).Registration()},
		Analyzers: []analyzer.Registration{{Analyzer: guest}},
	}
	result := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := application.RequiredTools(ctx, options, []string{"ci.yml"})
		result <- err
	}()
	t.Cleanup(func() {
		close(release)
		<-finished
	})
	select {
	case negotiationContext := <-guest.contexts:
		if negotiationContext.Done() == nil {
			t.Fatal("planning negotiation discarded cancellation")
		}
	case err := <-result:
		t.Fatalf("planning returned before negotiation: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("planning lost cancellation: %v", err)
	}
}
