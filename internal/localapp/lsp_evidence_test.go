package localapp

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

func capturedEditorSource(t testing.TB, path string, content []byte, model source.Coordinates) report.CapturedSource {
	t.Helper()
	revision, err := source.NewRevision(sourceid.Document(path), sourceid.Digest(content))
	if err != nil {
		t.Fatal(err)
	}
	return report.CapturedSource{Revision: revision, Coordinates: model, CoordinatesAvailable: utf8.Valid(content), Content: content}
}

func TestEditorCaseVariantBuffersConflictOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows case-insensitive drive alias regression")
	}
	root := t.TempDir()
	if len(filepath.VolumeName(root)) != 2 {
		t.Skipf("host temporary path has no drive: %q", root)
	}
	path := filepath.Join(root, "action.yml")
	if err := os.WriteFile(path, []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := strings.ToLower(path[:1]) + path[1:]
	if alias == path {
		alias = strings.ToUpper(path[:1]) + path[1:]
	}
	left, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	right, err := os.Stat(alias)
	if err != nil || !os.SameFile(left, right) {
		t.Fatalf("fixture aliases are not the same host file: %v", err)
	}
	caller := filepath.Join(root, "workflow.yml")
	callerText := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
	first, second := "name: first\ndescription: first\nruns: {using: composite, steps: []}\n", "name: second\ndescription: second\nruns: {using: composite, steps: []}\n"
	for _, reverse := range []bool{false, true} {
		a, b := first, second
		if reverse {
			a, b = b, a
		}
		analysis, err := analyzeDocuments(t.Context(), application.Options{WorkingDir: root, SkipProjectConfig: true}, map[string]langserver.Document{path: {Text: a}, alias: {Text: b}, caller: {Text: callerText}})
		if _, conflicted := errors.AsType[*capture.ConflictError](err); !conflicted {
			t.Fatalf("case-variant buffers were not quarantined: %v", err)
		}
		if _, exists := analysis.Sources[path]; exists {
			t.Fatal("conflicted physical spelling supplied text")
		}
		if _, exists := analysis.Sources[alias]; exists {
			t.Fatal("conflicted case alias supplied text")
		}
		if analysis.Sources[caller].Text != callerText {
			t.Fatal("case conflict discarded healthy caller source")
		}
		for _, finding := range analysis.Diagnostics {
			if finding.Path == path || finding.Path == alias {
				t.Fatal("case conflict fabricated a range")
			}
		}
	}
}

func TestEditorAdapterHonorsAlignedCoordinateEvidence(t *testing.T) {
	for _, test := range []struct {
		name                       string
		content                    []byte
		model                      source.Coordinates
		raw, unavailable, admitted bool
	}{
		{name: "known YAML", content: []byte("😀bad\n"), model: source.YAML, admitted: true},
		{name: "raw LF equivalent", content: []byte("😀bad\n"), model: source.YAML, raw: true, admitted: true},
		{name: "unavailable matching captured buffer", content: []byte("😀bad\n"), model: source.YAML, unavailable: true},
		{name: "raw LF lone CR", content: []byte("first\rsecond\nbad"), model: source.YAML, raw: true},
		{name: "non YAML capture", content: []byte("😀bad\n"), model: source.RawLF, raw: true},
		{name: "invalid UTF8 capture", content: []byte{0xff}, model: source.YAML},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "workflow.yml")
			captured := capturedEditorSource(t, path, test.content, test.model)
			result := &application.Result{Diagnostics: []diagnostic.Diagnostic{{Path: path, Rule: "evidence", Start: diagnostic.Position{Line: 1, Column: 2}, Reference: &source.Reference{Document: captured.Revision.Document(), Revision: captured.Revision.Digest()}}}, Sources: []report.DiagnosticSource{{Content: test.content, RawScript: test.raw, Unavailable: test.unavailable}}, CapturedSources: map[string]report.CapturedSource{path: captured}}
			analysis, err := editorAnalysis(result, root, map[string]langserver.Document{path: {Text: string(test.content)}})
			if test.admitted {
				if err != nil || len(analysis.Diagnostics) != 1 {
					t.Fatalf("exact coordinate evidence was lost: %+v %v", analysis, err)
				}
			} else if failure, ok := errors.AsType[*editorEvidenceError](err); !ok || len(failure.paths) != 1 || len(analysis.Diagnostics) != 0 {
				t.Fatalf("unavailable coordinate evidence was promoted: %+v %v", analysis, err)
			}
		})
	}
}

func TestEditorDriveCaseAliasKeepsCapturedIdentity(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows drive-case alias regression")
	}
	root := t.TempDir()
	if len(filepath.VolumeName(root)) != 2 {
		t.Skipf("host temporary directory does not use a drive: %q", root)
	}
	aliasRoot := strings.ToLower(root[:1]) + root[1:]
	if aliasRoot == root {
		aliasRoot = strings.ToUpper(root[:1]) + root[1:]
	}
	path := filepath.Join(aliasRoot, "workflow.yml")
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.Join(root, relative) == path {
		t.Fatalf("fixture did not exercise the Windows display/capture mismatch: %q %v", relative, err)
	}
	text := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: 'echo 😀::set-output name=result::value'}]}}\n"
	analysis, err := analyzeDocuments(t.Context(), application.Options{WorkingDir: root, SkipProjectConfig: true}, map[string]langserver.Document{path: {URI: langserver.FileURI(path), Version: 4, Text: text}})
	if err != nil || len(analysis.Diagnostics) != 1 || analysis.Diagnostics[0].Path != path || analysis.Sources[path].Text != text {
		t.Fatalf("case alias lost its captured identity: %+v %v", analysis, err)
	}
	if analysis.Diagnostics[0].Reference == nil || analysis.Diagnostics[0].Reference.Revision != analysis.Sources[path].Revision.Digest() {
		t.Fatal("published alias was not bound to its captured revision")
	}
}
