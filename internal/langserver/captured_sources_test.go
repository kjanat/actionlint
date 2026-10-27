package langserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestCapturedTextDefinesOpenDocumentRangesAndConflictClears(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Serve(t.Context(), server, server, func(_ context.Context, documents map[string]Document) (Analysis, error) {
			for path, document := range documents {
				finding := diagnostic.Diagnostic{Path: path, Rule: "captured", Message: "captured finding", Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 5}}
				if document.Version == 2 {
					// A conflicted capture has no publishable source, even though
					// the editor still owns text and version metadata for it.
					return Analysis{Diagnostics: []diagnostic.Diagnostic{finding}}, errors.New("source-revision-conflict: conflicting overlays")
				}
				if document.Version == 3 {
					finding.Related = []diagnostic.Related{{Path: path + ".related", Message: "unavailable related source", Start: diagnostic.Position{Line: 1, Column: 2}}}
				}
				return Analysis{Diagnostics: []diagnostic.Diagnostic{finding}, Sources: map[string]string{path: "\ufeff😀bad\n"}}, nil
			}
			return Analysis{}, nil
		})
	}()
	reader := bufio.NewReader(client)
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
	type envelope struct {
		Method string `json:"method"`
		Params struct {
			URI         string `json:"uri"`
			Version     int    `json:"version"`
			Message     string `json:"message"`
			Diagnostics []struct {
				Range   sourceRange       `json:"range"`
				Related []json.RawMessage `json:"relatedInformation"`
			} `json:"diagnostics"`
		} `json:"params"`
	}
	read := func() envelope {
		t.Helper()
		raw, err := readRawEnvelope(reader)
		if err != nil {
			t.Fatal(err)
		}
		var response envelope
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	send("initialize", 1, nil)
	read()
	uri := strings.Replace(testFileURI(t), "workflow", "work%66low", 1)
	send("textDocument/didOpen", nil, map[string]any{"textDocument": Document{URI: uri, Version: 1, Text: "abcbad\n"}})
	publication := read()
	if publication.Method != "textDocument/publishDiagnostics" || publication.Params.URI != uri || publication.Params.Version != 1 || len(publication.Params.Diagnostics) != 1 || publication.Params.Diagnostics[0].Range.Start.Character != 2 {
		t.Fatalf("editor metadata replaced captured coordinates: %+v", publication)
	}
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 2}, "contentChanges": []any{map[string]string{"text": "still open"}}})
	logged := read()
	if logged.Method != "window/logMessage" || !strings.Contains(logged.Params.Message, "source-revision-conflict") || !strings.Contains(logged.Params.Message, "diagnostic sources unavailable for 1 paths") {
		t.Fatalf("missing captured source was silently discarded: %+v", logged)
	}
	publication = read()
	if publication.Method != "textDocument/publishDiagnostics" || publication.Params.URI != uri || publication.Params.Version != 2 || len(publication.Params.Diagnostics) != 0 {
		t.Fatalf("conflicted capture was rendered from editor text: %+v", publication)
	}
	send("textDocument/didChange", nil, map[string]any{"textDocument": Document{URI: uri, Version: 3}, "contentChanges": []any{map[string]string{"text": "latest open"}}})
	logged = read()
	if logged.Method != "window/logMessage" || !strings.Contains(logged.Params.Message, "diagnostic sources unavailable for 1 paths") {
		t.Fatalf("missing related capture was silently discarded: %+v", logged)
	}
	publication = read()
	if publication.Params.Version != 3 || len(publication.Params.Diagnostics) != 1 || len(publication.Params.Diagnostics[0].Related) != 0 || publication.Params.Diagnostics[0].Range.Start.Character != 2 {
		t.Fatalf("missing related source invented coordinates: %+v", publication)
	}
	send("shutdown", 3, nil)
	read()
	send("exit", nil, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
