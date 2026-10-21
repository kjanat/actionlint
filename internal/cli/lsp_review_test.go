package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/internal/langserver"
)

type lspTestMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func TestLSPRepositoryProcessOptIn(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, name := range []string{"ACTIONLINT_RUFF_BIN", "ACTIONLINT_RUFF_FLAGS", "ACTIONLINT_RUFF_ENV"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "executed")
	config := "tools:\n  ruff:\n    backend:\n      process:\n        executable: " + strconv.Quote(executable) + "\n        args: ['-test.run=^TestRepositoryRuffCommandHelper$', '--']\n        env:\n          ACTIONLINT_REPO_RUFF_HELPER: '1'\n          ACTIONLINT_REPO_RUFF_MARKER: " + strconv.Quote(marker) + "\n"
	if err := os.WriteFile("actionlint.yaml", []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, allow := range []bool{false, true} {
		t.Run(strconv.FormatBool(allow), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			command := Command{Stdin: server, Stdout: server, Stderr: &stderr}
			args := []string{"actionlint", "lsp", "--config=actionlint.yaml", "--no-shellcheck"}
			if allow {
				args = append(args, "--allow-config-processes")
			}
			done := make(chan int, 1)
			go func() { done <- command.MainContext(ctx, args) }()
			reader := bufio.NewReader(client)
			send := func(method string, id, params json.RawMessage) {
				t.Helper()
				data, err := json.Marshal(lspTestMessage{JSONRPC: "2.0", ID: id, Method: method, Params: params})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Fprintf(client, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
					t.Fatal(err)
				}
			}
			receive := func() lspTestMessage {
				t.Helper()
				length := 0
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						t.Fatal(err)
					}
					if strings.TrimSpace(line) == "" {
						break
					}
					if value, ok := strings.CutPrefix(line, "Content-Length:"); ok {
						length, err = strconv.Atoi(strings.TrimSpace(value))
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				data := make([]byte, length)
				if _, err := io.ReadFull(reader, data); err != nil {
					t.Fatal(err)
				}
				var message lspTestMessage
				if err := json.Unmarshal(data, &message); err != nil {
					t.Fatal(err)
				}
				return message
			}
			send("initialize", json.RawMessage(`1`), json.RawMessage(`{}`))
			receive()
			params, err := json.Marshal(struct {
				Document langserver.Document `json:"textDocument"`
			}{langserver.Document{URI: langserver.FileURI(filepath.Join(root, "ci.yml")), Version: 1, Text: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(1)\n"}})
			if err != nil {
				t.Fatal(err)
			}
			send("textDocument/didOpen", nil, params)
			var logs []string
			for {
				message := receive()
				if message.Method == "textDocument/publishDiagnostics" {
					break
				}
				if message.Method == "window/logMessage" {
					var log struct{ Message string }
					if err := json.Unmarshal(message.Params, &log); err != nil {
						t.Fatal(err)
					}
					logs = append(logs, log.Message)
				}
			}
			logged := strings.Join(logs, "\n")
			if allow {
				if logged != "" {
					t.Errorf("host opt-in failed: %s", logged)
				}
				if _, err := os.Stat(marker); err != nil {
					t.Errorf("approved LSP process did not execute: %v", err)
				}
			} else {
				if !strings.Contains(logged, "--allow-config-processes") {
					t.Errorf("missing process opt-in error: %s", logged)
				}
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("untrusted LSP configuration executed: %v", err)
				}
			}
			send("shutdown", json.RawMessage(`2`), nil)
			receive()
			send("exit", nil, nil)
			if status := <-done; status != 0 {
				t.Errorf("LSP exit=%d stderr=%s", status, &stderr)
			}
		})
	}
}
