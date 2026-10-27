package localapp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/source"
)

type editorPublication struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params struct {
		URI         string `json:"uri"`
		Version     int    `json:"version"`
		Message     string `json:"message"`
		Diagnostics []struct {
			Range struct {
				Start struct{ Line, Character int } `json:"start"`
			} `json:"range"`
		} `json:"diagnostics"`
	} `json:"params"`
}

func firstEditorPublication(t testing.TB, analyze langserver.Analyze, uri, text string) (editorPublication, []string) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- langserver.Serve(t.Context(), server, server, analyze) }()
	send := func(method string, id int, params any) {
		t.Helper()
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != 0 {
			message["id"] = id
		}
		body, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(client, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(client)
	read := func() editorPublication {
		t.Helper()
		var size int
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &size); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		var message editorPublication
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatal(err)
		}
		return message
	}
	send("initialize", 1, nil)
	if message := read(); message.ID != 1 {
		t.Fatalf("initialization response missing: %+v", message)
	}
	send("textDocument/didOpen", 0, map[string]any{"textDocument": langserver.Document{URI: uri, Version: 17, Text: text}})
	var publication editorPublication
	var logs []string
	for {
		message := read()
		if message.Method == "window/logMessage" {
			logs = append(logs, message.Params.Message)
			continue
		}
		if message.Method != "textDocument/publishDiagnostics" {
			t.Fatalf("unexpected message: %+v", message)
		}
		publication = message
		break
	}
	send("shutdown", 2, nil)
	if message := read(); message.ID != 2 {
		t.Fatalf("unexpected message before shutdown: %+v", message)
	}
	send("exit", 0, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return publication, logs
}

func TestEditorCaseAliasPublishesCapturedUTF16WithOriginalURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows drive-case URI regression")
	}
	root := t.TempDir()
	if len(filepath.VolumeName(root)) != 2 {
		t.Skipf("host temporary path has no drive: %q", root)
	}
	aliasRoot := strings.ToLower(root[:1]) + root[1:]
	if aliasRoot == root {
		aliasRoot = strings.ToUpper(root[:1]) + root[1:]
	}
	path := filepath.Join(aliasRoot, "workflow.yml")
	uri := strings.Replace(langserver.FileURI(path), path[:2], path[:1]+"%3A", 1)
	text := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: 'echo 😀::set-output name=result::value'}]}}\n"
	options := application.Options{WorkingDir: root, SkipProjectConfig: true}
	analysis, err := analyzeDocuments(t.Context(), options, map[string]langserver.Document{path: {Text: text}})
	if err != nil || len(analysis.Diagnostics) != 1 {
		t.Fatalf("fixture analysis: %+v %v", analysis, err)
	}
	publication, logs := firstEditorPublication(t, func(ctx context.Context, documents map[string]langserver.Document) (langserver.Analysis, error) {
		return analyzeDocuments(ctx, options, documents)
	}, uri, text)
	if len(logs) != 0 || publication.Params.URI != uri || publication.Params.Version != 17 || len(publication.Params.Diagnostics) != 1 {
		t.Fatalf("case alias was withheld or lost metadata: publication=%+v logs=%v", publication, logs)
	}
	start := publication.Params.Diagnostics[0].Range.Start
	// The emoji preceding the command occupies one source column and two UTF-16 units.
	if start.Line != analysis.Diagnostics[0].Start.Line-1 || start.Character != analysis.Diagnostics[0].Start.Column {
		t.Fatalf("captured UTF-16 range lost: %+v source=%+v", start, analysis.Diagnostics[0].Start)
	}
}

func TestEditorUnavailableAndRawMismatchLogWithoutPublishingRanges(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		name := "raw LF mismatch"
		if unavailable {
			name = "unavailable matching captured row"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path, text := filepath.Join(root, "workflow.yml"), "first\rsecond\nbad"
			captured := capturedEditorSource(t, path, []byte(text), source.YAML)
			result := &application.Result{Diagnostics: []diagnostic.Diagnostic{{Path: path, Start: diagnostic.Position{Line: 2, Column: 1}, Reference: &source.Reference{Document: captured.Revision.Document(), Revision: captured.Revision.Digest()}}}, Sources: []report.DiagnosticSource{{Content: []byte(text), RawScript: !unavailable, Unavailable: unavailable}}, CapturedSources: map[string]report.CapturedSource{path: captured}}
			publication, logs := firstEditorPublication(t, func(_ context.Context, documents map[string]langserver.Document) (langserver.Analysis, error) {
				return editorAnalysis(result, root, documents)
			}, langserver.FileURI(path), text)
			if len(publication.Params.Diagnostics) != 0 || len(logs) != 1 || !strings.Contains(logs[0], "incomplete editor analysis") {
				t.Fatalf("unavailable evidence produced a range or silent success: publication=%+v logs=%v", publication, logs)
			}
		})
	}
}
