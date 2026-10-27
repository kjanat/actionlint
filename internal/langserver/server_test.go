package langserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/sourceid"
)

func TestUnsupportedRequestsReceiveMethodNotFound(t *testing.T) {
	var input, output bytes.Buffer
	for _, request := range []message{
		{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"},
		{JSONRPC: "2.0", Method: "unsupported/notification"},
		{JSONRPC: "2.0", ID: json.RawMessage(`"hover-request"`), Method: "textDocument/hover"},
		{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "shutdown"},
		{JSONRPC: "2.0", Method: "exit"},
	} {
		if err := writeMessage(&input, request); err != nil {
			t.Fatal(err)
		}
	}
	if err := Serve(t.Context(), &input, &output, func(context.Context, map[string]Document) (Analysis, error) {
		return Analysis{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	for _, want := range []struct {
		id   string
		code int
	}{{`1`, 0}, {`"hover-request"`, -32601}, {`2`, 0}} {
		raw, err := readRawEnvelope(reader)
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID    json.RawMessage `json:"id"`
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if string(response.ID) != want.id || response.Error.Code != want.code {
			t.Fatalf("response=%s, want id=%s error=%d", raw, want.id, want.code)
		}
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("unexpected notification response: %v", err)
	}
}

func TestVersionedBuffersDiagnosticsAndQuickFix(t *testing.T) {
	in, client := io.Pipe()
	receive, out := io.Pipe()
	defer client.Close()
	defer receive.Close()
	done := make(chan error, 1)
	go func() {
		done <- Serve(t.Context(), in, out, func(ctx context.Context, docs map[string]Document) (Analysis, error) {
			var result []diagnostic.Diagnostic
			for path, doc := range docs {
				if !strings.Contains(doc.Text, "bad") {
					continue
				}
				result = append(result, diagnostic.Diagnostic{Path: path, Rule: "test", Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 5}, Message: "bad", Fixes: []diagnostic.Fix{{Description: "replace", Applicability: "safe", Revisions: map[string]string{path: sourceid.Digest([]byte(doc.Text))}, Edits: []diagnostic.Edit{{Path: path, Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 5}, Replacement: "good"}}}}})
			}
			sources := make(map[string]string, len(docs))
			for path, doc := range docs {
				sources[path] = doc.Text
			}
			return Analysis{Diagnostics: result, Sources: sources}, nil
		})
	}()
	reader := bufio.NewReader(receive)
	send := func(method string, id any, params any) {
		t.Helper()
		m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			m["id"] = id
		}
		if err := writeMessage(client, m); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		m, err := readEnvelope(reader)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	send("initialize", 1, map[string]any{})
	if read()["result"] == nil {
		t.Fatal("no initialize response")
	}
	uri := testFileURI(t)
	send("textDocument/didOpen", nil, map[string]any{"textDocument": Document{uri, 1, "😀bad\n"}})
	params := read()["params"].(map[string]any)
	items := params["diagnostics"].([]any)
	if len(items) != 1 {
		t.Fatal(items)
	}
	start := items[0].(map[string]any)["range"].(map[string]any)["start"].(map[string]any)
	if start["character"] != float64(2) {
		t.Fatal(start)
	}
	send("textDocument/codeAction", 2, map[string]any{"textDocument": map[string]string{"uri": uri}, "range": sourceRange{position{0, 0}, position{0, 9}}})
	actions := read()["result"].([]any)
	if len(actions) != 1 {
		t.Fatal(actions)
	}
	changes := actions[0].(map[string]any)["edit"].(map[string]any)["documentChanges"].([]any)
	if changes[0].(map[string]any)["textDocument"].(map[string]any)["version"] != float64(1) {
		t.Fatal(changes)
	}
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 2}, "contentChanges": []any{map[string]any{"range": sourceRange{position{0, 2}, position{0, 5}}, "text": "good"}}})
	params = read()["params"].(map[string]any)
	if params["version"] != float64(2) || len(params["diagnostics"].([]any)) != 0 {
		t.Fatal(params)
	}
	send("shutdown", 3, nil)
	read()
	send("exit", nil, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

// readEnvelope retains the response result; message is intentionally only the
// request decoder used by the server.
func readEnvelope(r *bufio.Reader) (map[string]any, error) {
	m, err := readRawEnvelope(r)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	err = json.Unmarshal(m, &value)
	return value, err
}

func readRawEnvelope(r *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			if _, err := fmt.Sscanf(line, "Content-Length: %d", &length); err != nil {
				return nil, err
			}
		}
	}
	data := make([]byte, length)
	_, err := io.ReadFull(r, data)
	return data, err
}

func TestCoordinateBoundaries(t *testing.T) {
	if _, err := byteOffset("😀x", position{0, 1}); err == nil {
		t.Fatal("accepted middle of surrogate pair")
	}
	if offset, err := byteOffset("😀x", position{0, 2}); err != nil || offset != 4 {
		t.Fatalf("%d %v", offset, err)
	}
	if _, err := readMessage(bufio.NewReader(strings.NewReader("Content-Length: 999999999\r\n\r\n"))); err == nil {
		t.Fatal("accepted oversized frame")
	}
}

func TestSupersededAnalysisCannotPublish(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(release) })
	defer releaseOld()
	finished := make(chan error, 1)
	go func() {
		finished <- Serve(t.Context(), server, server, func(ctx context.Context, docs map[string]Document) (Analysis, error) {
			for path, doc := range docs {
				if doc.Version == 1 {
					close(started)
					<-ctx.Done()
					close(cancelled)
					<-release
					return Analysis{Diagnostics: []diagnostic.Diagnostic{{Path: path, Message: "stale"}}, Sources: map[string]string{path: doc.Text}}, errors.New("source-revision-conflict: stale generation")
				}
			}
			return Analysis{}, nil
		})
	}()
	read := bufio.NewReader(client)
	send := func(method string, id any, params any) {
		t.Helper()
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			message["id"] = id
		}
		if err := writeMessage(client, message); err != nil {
			t.Fatal(err)
		}
	}
	receive := func() map[string]any {
		t.Helper()
		value, err := readEnvelope(read)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	send("initialize", 1, nil)
	receive()
	uri := testFileURI(t)
	send("textDocument/didOpen", nil, map[string]any{"textDocument": Document{URI: uri, Version: 1, Text: "before"}})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("analysis did not start")
	}
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 2}, "contentChanges": []any{map[string]string{"text": "after"}}})
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("analysis was not cancelled")
	}
	params := receive()["params"].(map[string]any)
	if params["version"] != float64(2) || len(params["diagnostics"].([]any)) != 0 {
		t.Fatal(params)
	}
	// An invalid ranged update is rejected without changing version 2 or killing the server.
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 3}, "contentChanges": []any{map[string]any{"range": sourceRange{position{0, 99}, position{0, 100}}, "text": "x"}}})
	if receive()["method"] != "window/logMessage" {
		t.Fatal("invalid notification was not reported")
	}
	send("textDocument/codeAction", 2, map[string]any{"textDocument": map[string]string{"uri": uri}})
	if receive()["id"] != float64(2) {
		t.Fatal("server did not respond after malformed notification")
	}
	releaseOld()
	send("shutdown", 3, nil)
	receive()
	send("exit", nil, nil)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func testFileURI(t *testing.T) string {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "workflow.yml"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func TestDependencyDiagnosticsPublishedAndCleared(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	uri, dependencyURI, relatedURI := testFileURI(t), testFileURI(t), testFileURI(t)
	dependency, err := filePath(dependencyURI)
	if err != nil {
		t.Fatal(err)
	}
	related, err := filePath(relatedURI)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Serve(t.Context(), server, server, func(_ context.Context, documents map[string]Document) (Analysis, error) {
			for _, doc := range documents {
				if doc.Text == "uses dependency" {
					return Analysis{
						Sources:     map[string]string{dependency: "\ufeff😀bad\n", related: "\ufeff😀related\n"},
						Diagnostics: []diagnostic.Diagnostic{{Path: dependency, Rule: "test", Message: "bad", Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 5}, Related: []diagnostic.Related{{Path: related, Message: "related", Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 9}}}}},
					}, nil
				}
			}
			return Analysis{}, nil
		})
	}()
	reader := bufio.NewReader(client)
	send := func(method string, id any, params any) {
		t.Helper()
		m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			m["id"] = id
		}
		if err := writeMessage(client, m); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		m, err := readEnvelope(reader)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	publications := func() map[string]map[string]any {
		t.Helper()
		result := map[string]map[string]any{}
		for range 2 {
			m := read()
			if m["method"] != "textDocument/publishDiagnostics" {
				t.Fatalf("unexpected message: %+v", m)
			}
			p := m["params"].(map[string]any)
			result[p["uri"].(string)] = p
		}
		return result
	}
	send("initialize", 1, nil)
	read()
	send("textDocument/didOpen", nil, map[string]any{"textDocument": Document{URI: uri, Version: 1, Text: "uses dependency"}})
	published := publications()
	p := published[dependencyURI]
	if _, hasVersion := p["version"]; hasVersion {
		t.Fatal("closed dependency received an editor version")
	}
	items, ok := p["diagnostics"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("missing dependency finding: %+v", published)
	}
	item := items[0].(map[string]any)
	start := item["range"].(map[string]any)["start"].(map[string]any)
	if start["character"] != float64(2) {
		t.Fatalf("dependency source was not used for UTF-16: %+v", start)
	}
	locations := item["relatedInformation"].([]any)
	location := locations[0].(map[string]any)["location"].(map[string]any)
	if location["uri"] != relatedURI || location["range"].(map[string]any)["start"].(map[string]any)["character"] != float64(2) {
		t.Fatalf("closed related source lost: %+v", location)
	}
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 2}, "contentChanges": []any{map[string]string{"text": "no dependencies"}}})
	published = publications()
	if len(published[dependencyURI]["diagnostics"].([]any)) != 0 || published[uri]["version"] != float64(2) {
		t.Fatalf("removed dependency was not cleared: %+v", published)
	}
	// Re-add the dependency, then close its only caller. Both publications clear.
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 3}, "contentChanges": []any{map[string]string{"text": "uses dependency"}}})
	publications()
	send("textDocument/didClose", nil, map[string]any{"textDocument": Document{URI: uri}})
	published = publications()
	for _, p := range published {
		if len(p["diagnostics"].([]any)) != 0 {
			t.Fatalf("closed caller left stale diagnostics: %+v", published)
		}
	}
	send("shutdown", 2, nil)
	read()
	send("exit", nil, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
