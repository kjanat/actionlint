package langserver

import (
	"bufio"
	"context"
	"io"
	"testing"
)

func TestCompletionUsesCurrentBufferAndUTF16(t *testing.T) {
	input, client := io.Pipe()
	receive, output := io.Pipe()
	defer client.Close()
	defer receive.Close()
	done := make(chan error, 1)
	go func() {
		done <- Serve(t.Context(), input, output,
			func(context.Context, map[string]Document) (Analysis, error) { return Analysis{}, nil },
			func(_ context.Context, doc Document, offset int) []Completion {
				// The emoji occupies four UTF-8 bytes and two UTF-16 units.
				if doc.Version != 2 || doc.Text != "😀titel" || offset != 6 {
					return nil
				}
				return []Completion{{Label: "title", Detail: "string", Start: 4, End: 9}}
			})
	}()
	reader := bufio.NewReader(receive)
	request := func(method string, id int, params any) map[string]any {
		t.Helper()
		if err := writeMessage(client, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for {
			m, err := readEnvelope(reader)
			if err != nil {
				t.Fatal(err)
			}
			if m["id"] == float64(id) {
				return m
			}
		}
	}
	init := request("initialize", 1, map[string]any{})
	if init["result"].(map[string]any)["capabilities"].(map[string]any)["completionProvider"] == nil {
		t.Fatal(init)
	}
	uri := testFileURI(t)
	for _, change := range []struct {
		method string
		params any
	}{
		{"textDocument/didOpen", map[string]any{"textDocument": Document{uri, 1, "old"}}},
		{"textDocument/didChange", map[string]any{"textDocument": Document{URI: uri, Version: 2}, "contentChanges": []any{map[string]any{"text": "😀titel"}}}},
	} {
		if err := writeMessage(client, map[string]any{"jsonrpc": "2.0", "method": change.method, "params": change.params}); err != nil {
			t.Fatal(err)
		}
		// Drain the diagnostic publication before sending the next message on pipes.
		if _, err := readEnvelope(reader); err != nil {
			t.Fatal(err)
		}
	}
	result := request("textDocument/completion", 2, map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position{0, 4}})
	items := result["result"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatal(result)
	}
	edit := items[0].(map[string]any)["textEdit"].(map[string]any)
	span := edit["range"].(map[string]any)
	if span["start"].(map[string]any)["character"] != float64(2) || span["end"].(map[string]any)["character"] != float64(7) {
		t.Fatal(span)
	}
	request("shutdown", 3, nil)
	if err := writeMessage(client, map[string]any{"jsonrpc": "2.0", "method": "exit"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
