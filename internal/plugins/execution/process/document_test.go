package process

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func documentInput(t *testing.T, content string) checkinputs.Input {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "workflow.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	revision, err := sourcepos.NewRevision(path, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return checkinputs.Input{Path: path, Directory: directory, Content: content, Kind: "workflow", Revision: revision, Coordinates: sourcepos.YAML, Reference: sourcepos.Reference{Document: path, Revision: revision.Digest(), Node: "root", Address: "/"}, AvailableInputs: []rulemeta.InputKind{rulemeta.Source, rulemeta.Dependencies}}
}

func documentManifest(spec manifests.ProcessSpec) manifests.Manifest {
	manifest := helperManifest(spec)
	manifest.Role, manifest.Preparation = manifests.RoleDocument, "generic"
	manifest.Selectors.Documents = []string{"workflow"}
	return manifest
}

func TestDocumentDescriptorConfigurationScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scope manifests.Scope
		want  rulemeta.ConfigurationScope
	}{{manifests.ScopeSource, rulemeta.SourceConfiguration}, {manifests.ScopeComposite, rulemeta.CompositeConfiguration}} {
		for _, documents := range [][]string{nil, {"workflow"}, {"action"}, {"workflow", "action"}} {
			manifest := documentManifest(manifests.ProcessSpec{Output: echoOutput})
			manifest.Scope, manifest.Selectors.Documents = tc.scope, documents
			checker, err := NewDocumentAnalyzer(manifest, helperCommand(t, "echo"), Options{})
			if err != nil {
				t.Fatal(err)
			}
			descriptor := checker.Descriptor()
			wantActions := len(documents) == 0 || slices.Contains(documents, "action")
			if descriptor.Scope != tc.want || descriptor.SupportsActions != wantActions {
				t.Fatalf("scope=%v documents=%v: %+v", tc.scope, documents, descriptor)
			}
		}
	}
}

func TestDocumentProcessOriginalSourceAndRequestFacts(t *testing.T) {
	t.Parallel()
	for _, stdin := range []manifests.Stdin{manifests.StdinSource, manifests.StdinRequest} {
		t.Run(string(stdin), func(t *testing.T) {
			t.Parallel()
			input := documentInput(t, "on: push\njobs:\n  one:\n    if: ${{ github.actor == 'raw' }}\n")
			input.Options = json.RawMessage(`{"mode":"checked"}`)
			input.Dependencies = []workflowfacts.Dependency{{Reference: input.Reference, Uses: "owner/action@v1", Kind: "action", Start: sourcepos.Position{Line: 2, Column: 1}, End: sourcepos.Position{Line: 2, Column: 5}}, {Reference: sourcepos.Reference{Document: "foreign"}, Uses: "foreign/action@v1"}}
			manifest := documentManifest(manifests.ProcessSpec{Args: []string{"--"}, Stdin: stdin, Output: echoOutput, Config: manifests.ConfigRendering{Render: manifests.RenderNone}})
			manifest.Rule.RequiredInputs = []rulemeta.InputKind{rulemeta.Dependencies}
			manifest.Capabilities.Resources = []manifests.Resource{manifests.ResourceDirectory}
			checker, err := NewDocumentAnalyzer(manifest, helperCommand(t, "echo"), Options{})
			if err != nil {
				t.Fatal(err)
			}
			findings, err := checker.CheckDocument(t.Context(), input)
			if err != nil || len(findings) != 1 {
				t.Fatal(findings, err)
			}
			var echo echoReport
			if err := json.Unmarshal([]byte(findings[0].Message), &echo); err != nil {
				t.Fatal(err)
			}
			if stdin == manifests.StdinSource {
				if echo.Report.Stdin != input.Content {
					t.Fatal("document source was masked", echo.Report.Stdin)
				}
				return
			}
			var request checkinputs.DocumentRequest
			if err := json.Unmarshal([]byte(echo.Report.Stdin), &request); err != nil {
				t.Fatal(err)
			}
			if request.Source != input.Content || request.Filename != input.Path || request.Kind != "workflow" || request.Directory != input.Directory || string(request.Config) != string(input.Options) || len(request.Dependencies) != 1 || request.Dependencies[0].Reference != input.Reference || request.Dependencies[0].Start != input.Dependencies[0].Start {
				t.Fatal("document facts changed", request)
			}
			if strings.Contains(echo.Report.Stdin, "SourceRoots") || strings.Contains(echo.Report.Stdin, "DirectoryFS") {
				t.Fatal("host resources leaked")
			}
		})
	}
}

func TestDocumentProcessCoordinatesFixesAndForeignFiles(t *testing.T) {
	t.Parallel()
	input := documentInput(t, "head\r\nαbc\rdef\n")
	pool := process.NewProcessPool(t.Context(), 1)
	command := pool.EmbeddedCommand("fixture", func([]string, string) ([]byte, error) {
		return []byte(`{"ok":true,"diagnostics":[{"code":"D1","message":"original document","location":{"row":3,"column":1},"end_location":{"row":3,"column":4},"fixes":[{"description":"replace","replacements":[{"row":3,"column":1,"end_row":3,"end_column":4,"text":"new"}]}]},{"filename":"other.yml","source":"other.yml","code":"OTHER","message":"foreign","location":{"row":1,"column":1}}]}`), nil
	})
	manifest := documentManifest(manifests.ProcessSpec{Stdin: manifests.StdinSource, Output: manifests.Output{Format: manifests.FormatEnvelope, Coordinates: &manifests.Coordinates{Rows: manifests.RowsSource, Columns: manifests.ColumnsBytes, End: manifests.EndExclusive}}})
	manifest.Capabilities.Fixes = true
	checker, err := NewDocumentAnalyzer(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := checker.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 1 {
		t.Fatal(findings, err)
	}
	finding := findings[0]
	if finding.Start != (sourcepos.Position{Line: 3, Column: 1}) || finding.End != (sourcepos.Position{Line: 3, Column: 4}) || finding.Path != input.Path || len(finding.Fixes) != 1 || finding.Fixes[0].Revisions[input.Path] != input.Revision.Digest() || finding.Fixes[0].Edits[0].Start != finding.Start {
		t.Fatal("document coordinates/fix provenance lost", finding)
	}
	manifest.Capabilities.Fixes = false
	checker, err = NewDocumentAnalyzer(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if findings, err := checker.CheckDocument(t.Context(), input); err == nil || len(findings) != 0 {
		t.Fatal("undeclared document fixes accepted", findings, err)
	}
}

func TestDocumentProcessSARIFAndJQ(t *testing.T) {
	t.Parallel()
	input := documentInput(t, "one\ntwo\n")
	for _, format := range []manifests.OutputFormat{manifests.FormatSARIF, manifests.FormatJSON} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			pool := process.NewProcessPool(t.Context(), 1)
			output := manifests.Output{Format: format}
			stdout := fmt.Sprintf(`{"version":"2.1.0","runs":[{"results":[{"ruleId":"D1","message":{"text":"document"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":%q},"region":{"startLine":2,"startColumn":2,"endColumn":3}}}]}]}]}`, input.Path)
			if format == manifests.FormatJSON {
				stdout = `{"text":"document"}`
				output.Transform = &manifests.Transform{Language: manifests.LanguageJQ, Inline: `[{filename:$vars.filename,code:"D1",message:.text,location:{row:2,column:2},end_location:{row:2,column:3}}]`}
			}
			manifest := documentManifest(manifests.ProcessSpec{Stdin: manifests.StdinSource, Output: output})
			command := pool.EmbeddedCommand("fixture", func([]string, string) ([]byte, error) { return []byte(stdout), nil })
			checker, err := NewDocumentAnalyzer(manifest, command, Options{})
			if err != nil {
				t.Fatal(err)
			}
			findings, err := checker.CheckDocument(t.Context(), input)
			if err != nil || len(findings) != 1 || findings[0].Start != (sourcepos.Position{Line: 2, Column: 2}) {
				t.Fatal(findings, err)
			}
		})
	}
}

func TestDocumentProcessFixApplicability(t *testing.T) {
	t.Parallel()
	input := documentInput(t, "one\n")
	for _, label := range []string{"safe", "unsafe", "suggested"} {
		pool := process.NewProcessPool(t.Context(), 1)
		output := manifests.Output{Format: manifests.FormatJSON, Transform: &manifests.Transform{
			Language: manifests.LanguageJQ,
			Inline:   `[{filename:$vars.filename,code:"D1",message:"fix",location:{row:1,column:1},fixes:[{applicability:"` + label + `",replacements:[{row:1,column:1,end_row:1,end_column:4,text:"two"}]}]}]`,
		}}
		manifest := documentManifest(manifests.ProcessSpec{Stdin: manifests.StdinSource, Output: output})
		manifest.Capabilities.Fixes = true
		command := pool.EmbeddedCommand("fixture", func([]string, string) ([]byte, error) { return []byte(`{}`), nil })
		checker, err := NewDocumentAnalyzer(manifest, command, Options{})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := checker.CheckDocument(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 || len(findings[0].Fixes) != 1 || findings[0].Fixes[0].Applicability.String() != label || findings[0].Fixes[0].Revisions[input.Path] != input.Revision.Digest() {
			t.Fatalf("lost document fix applicability or source binding: %+v", findings)
		}
	}
}

func TestDocumentProcessSelectionAndCapturedDisk(t *testing.T) {
	t.Parallel()
	input := documentInput(t, "one\n")
	pool := process.NewProcessPool(t.Context(), 1)
	calls := 0
	command := pool.EmbeddedCommand("fixture", func([]string, string) ([]byte, error) { calls++; return []byte(`{"ok":true,"diagnostics":[]}`), nil })
	manifest := documentManifest(manifests.ProcessSpec{Stdin: manifests.StdinSource, Output: manifests.Output{Format: manifests.FormatEnvelope}})
	manifest.Capabilities.Resources = []manifests.Resource{manifests.ResourceDirectory}
	checker, err := NewDocumentAnalyzer(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	unselected := input
	unselected.Kind = "action"
	if _, err := checker.CheckDocument(t.Context(), unselected); err != nil || calls != 0 {
		t.Fatal("unselected document ran", err)
	}
	if err := os.WriteFile(input.Path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checker.CheckDocument(t.Context(), input); err == nil || calls != 0 {
		t.Fatal("stale disk content executed", err)
	}
	manifest.Capabilities.Resources = nil
	checker, err = NewDocumentAnalyzer(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checker.CheckDocument(t.Context(), input); err != nil || calls != 1 {
		t.Fatal("stdin-only capture rejected", err)
	}
}

func TestDocumentProcessRejectsChangedDiskAfterExecution(t *testing.T) {
	t.Parallel()
	input := documentInput(t, "one\n")
	pool := process.NewProcessPool(t.Context(), 1)
	calls := 0
	command := pool.EmbeddedCommand("fixture", func([]string, string) ([]byte, error) {
		calls++
		if err := os.WriteFile(input.Path, []byte("changed\n"), 0o600); err != nil {
			return nil, err
		}
		return []byte(`{"ok":true,"diagnostics":[{"code":"D1","message":"stale finding","location":{"row":1,"column":1}}]}`), nil
	})
	manifest := documentManifest(manifests.ProcessSpec{Stdin: manifests.StdinSource, Output: manifests.Output{Format: manifests.FormatEnvelope}})
	manifest.Capabilities.Resources = []manifests.Resource{manifests.ResourceDirectory}
	checker, err := NewDocumentAnalyzer(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := checker.CheckDocument(t.Context(), input)
	if err == nil || !strings.Contains(err.Error(), "on-disk source to match captured content") || len(findings) != 0 || calls != 1 {
		t.Fatalf("changed disk admitted stale diagnostics: %+v, %v, calls=%d", findings, err, calls)
	}
}

func TestDocumentProcessCanceledFreshnessCheck(t *testing.T) {
	t.Parallel()
	input := documentInput(t, "one\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := currentDocument(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("freshness check ignored cancellation: %v", err)
	}
}

func TestDocumentForeignFileAuthority(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	filename := filepath.Join(directory, "current.yml")
	request := checkinputs.ScriptRequest{Filename: filename}
	foreign := "file://remote.example" + filepath.ToSlash(filename)
	if filepath.VolumeName(filename) != "" {
		foreign = "file://remote.example/" + filepath.ToSlash(filename)
	}
	findings := documentFindings([]checkinputs.ScriptDiagnostic{{Filename: foreign, Source: foreign}, {Filename: filename}}, request, directory)
	if len(findings) != 1 || findings[0].Filename != filename {
		t.Fatal("foreign file URI admitted as local capture", findings)
	}
}

func TestDocumentInvocationDefinitionsOwnTheirOptions(t *testing.T) {
	t.Parallel()
	manifest := documentManifest(manifests.ProcessSpec{Output: echoOutput})
	manifest.InvocationDefinitions = []commands.Definition{{ID: "example/runtime", Executables: []string{"custom"}, Options: []commands.Option{{Short: "o", Argument: true, Choices: []string{"allowed"}}}}}
	checker, err := NewDocumentAnalyzer(manifest, helperCommand(t, "echo"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	manifest.InvocationDefinitions[0].Options[0].Choices[0] = "caller-mutation"
	descriptor := checker.Descriptor()
	if descriptor.InvocationDefinitions[0].Options[0].Choices[0] != "allowed" {
		t.Fatal("constructor aliases provider options")
	}
	descriptor.InvocationDefinitions[0].Options[0].Choices[0] = "descriptor-mutation"
	if checker.Descriptor().InvocationDefinitions[0].Options[0].Choices[0] != "allowed" {
		t.Fatal("descriptor aliases provider options")
	}
}
