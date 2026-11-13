package lsp

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

	reportfixture "actionlint.kjanat.dev/v2/internal/development/tests/support/reportfixture"

	"actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type editorPublication struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
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

type editorExchange func(send func(string, int, any), read func() editorPublication)

func firstEditorPublication(tb testing.TB, analyze Analyze, uri, text string, exchanges ...editorExchange) (editorPublication, []string) {
	tb.Helper()
	server, client := net.Pipe()
	tb.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		tb.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(tb.Context(), server, server, analyze) }()
	send := func(method string, id int, params any) {
		tb.Helper()
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != 0 {
			message["id"] = id
		}
		body, err := json.Marshal(message)
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := fmt.Fprintf(client, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			tb.Fatal(err)
		}
	}
	reader := bufio.NewReader(client)
	read := func() editorPublication {
		tb.Helper()
		var size int
		line, err := reader.ReadString('\n')
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &size); err != nil {
			tb.Fatal(err)
		}
		if _, err := reader.ReadString('\n'); err != nil {
			tb.Fatal(err)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(reader, body); err != nil {
			tb.Fatal(err)
		}
		var message editorPublication
		if err := json.Unmarshal(body, &message); err != nil {
			tb.Fatal(err)
		}
		return message
	}
	send("initialize", 1, nil)
	if message := read(); message.ID != 1 {
		tb.Fatalf("initialization response missing: %+v", message)
	}
	send("textDocument/didOpen", 0, map[string]any{"textDocument": Document{URI: uri, Version: 17, Text: text}})
	var publication editorPublication
	var logs []string
	for {
		message := read()
		if message.Method == "window/logMessage" {
			logs = append(logs, message.Params.Message)
			continue
		}
		if message.Method != "textDocument/publishDiagnostics" {
			tb.Fatalf("unexpected message: %+v", message)
		}
		publication = message
		break
	}
	for _, exchange := range exchanges {
		exchange(send, read)
	}
	send("shutdown", 2, nil)
	if message := read(); message.ID != 2 {
		tb.Fatalf("unexpected message before shutdown: %+v", message)
	}
	send("exit", 0, nil)
	if err := <-done; err != nil {
		tb.Fatal(err)
	}
	return publication, logs
}

func TestEditorCaseAliasPublishesCapturedUTF16WithOriginalURI(t *testing.T) {
	t.Parallel()
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
	uri := strings.Replace(FileURI(path), path[:2], path[:1]+"%3A", 1)
	text := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: 'echo 😀::set-output name=result::value'}]}}\npermissions: {}\n"
	options := checking.Options{WorkingDir: root, SkipProjectConfig: true}
	analysis, err := analyzeDocuments(t.Context(), options, map[string]Document{path: {Text: text}})
	if err != nil || len(analysis.Diagnostics) != 1 {
		t.Fatalf("fixture analysis: %+v %v", analysis, err)
	}
	publication, logs := firstEditorPublication(t, func(ctx context.Context, documents map[string]Document) (Analysis, error) {
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
	t.Parallel()
	for _, name := range []string{"raw LF mismatch", "unavailable matching captured row", "non YAML captured row", "invalid UTF8 captured row"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path, text := filepath.Join(root, "workflow.yml"), "first\rsecond\nbad"
			model := sourcepos.YAML
			unavailable, raw := false, true
			switch name {
			case "unavailable matching captured row":
				unavailable, raw = true, false
			case "non YAML captured row":
				model, text = sourcepos.RawLF, "first\nbad"
			case "invalid UTF8 captured row":
				text, raw = string([]byte{0xff}), false
			}
			captured := reportfixture.CapturedSource(t, path, []byte(text), model)
			result := &checking.Result{Diagnostics: []results.Diagnostic{{Path: path, Start: sourcepos.Position{Line: 2, Column: 1}, Reference: &sourcepos.Reference{Document: captured.Revision.Document(), Revision: captured.Revision.Digest()}}}, Sources: []report.DiagnosticSource{{Content: []byte(text), RawScript: raw, Unavailable: unavailable}}, CapturedSources: map[string]report.CapturedSource{path: captured}}
			publication, logs := firstEditorPublication(t, func(_ context.Context, documents map[string]Document) (Analysis, error) {
				return FromReport(result, root, documents)
			}, FileURI(path), text)
			if len(publication.Params.Diagnostics) != 0 || len(logs) != 1 || !strings.Contains(logs[0], "incomplete editor analysis") {
				t.Fatalf("unavailable evidence produced a range or silent success: publication=%+v logs=%v", publication, logs)
			}
		})
	}
}
