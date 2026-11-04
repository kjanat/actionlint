//go:build js && wasm

// Command actionlint-lsp-wasm hosts the shared language server in a browser worker.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"sync/atomic"
	"syscall/js"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

var requestSequence atomic.Uint64

func hostRequest(ctx context.Context, method string, params any) (js.Value, error) {
	if err := ctx.Err(); err != nil {
		return js.Undefined(), err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return js.Undefined(), err
	}
	id := strconv.FormatUint(requestSequence.Add(1), 10)
	type response struct {
		value js.Value
		err   error
	}
	responses := make(chan response, 1)
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any { responses <- response{value: args[0]}; return nil })
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		responses <- response{err: errors.New(js.Global().Get("String").Invoke(args[0]).String())}
		return nil
	})
	defer resolve.Release()
	defer reject.Release()
	js.Global().Call("actionlintHostRequest", id, method, string(encoded)).Call("then", resolve, reject)
	select {
	case result := <-responses:
		return result.value, result.err
	case <-ctx.Done():
		// The bridge rejects its promise when cancelled. Wait for that callback
		// before releasing the Go functions the JavaScript promise can call.
		js.Global().Call("actionlintCancelHostRequest", id)
		<-responses
		return js.Undefined(), ctx.Err()
	}
}

func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	var workspace langserver.WorkspaceURIs
	var tools []string
	return langserver.ServeWithOptions(ctx, in, out, langserver.Options{
		Initialize: func(raw json.RawMessage) error {
			var params struct {
				RootURI          string `json:"rootUri"`
				WorkspaceFolders []struct {
					URI string `json:"uri"`
				} `json:"workspaceFolders"`
				Options struct {
					Tools []string `json:"tools"`
				} `json:"initializationOptions"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				return err
			}
			root := params.RootURI
			if root == "" && len(params.WorkspaceFolders) > 0 {
				root = params.WorkspaceFolders[0].URI
			}
			if root == "" {
				root = "file:///workspace"
			}
			var err error
			workspace, err = langserver.NewWorkspaceURIs(root, "/workspace")
			if err != nil {
				return err
			}
			for _, name := range params.Options.Tools {
				if !slices.Contains(registry.IDs(), name) {
					return fmt.Errorf("no bundled tool provider is named %q", name)
				}
			}
			tools = slices.Clone(params.Options.Tools)
			return nil
		},
		PathFromURI: workspace.Path,
		URIFromPath: workspace.URI,
		Complete:    localapp.CompleteDocument,
		Analyze: func(ctx context.Context, documents map[string]langserver.Document) (langserver.Analysis, error) {
			selections := registry.Selections{}
			for _, name := range tools {
				selections[name] = toolSelection(ctx, name)
			}
			options := application.Options{WorkingDir: "/workspace", SkipProjectConfig: true, Tools: registry.Registrations(selections)}
			options.ReadFile = func(path string) ([]byte, error) {
				uri := workspace.URI(path)
				if uri == "" {
					return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrPermission}
				}
				value, err := hostRequest(ctx, "actionlint/readFile", struct {
					URI string `json:"uri"`
				}{uri})
				if err != nil {
					return nil, err
				}
				if value.IsNull() || value.IsUndefined() {
					return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrNotExist}
				}
				if value.Type() != js.TypeString {
					return nil, errors.New("workspace file response must be text or null")
				}
				return []byte(value.String()), nil
			}
			return localapp.AnalyzeEditorDocuments(ctx, options, documents)
		},
	})
}

func toolSelection(ctx context.Context, name string) backend.Selection {
	return backend.Selection{Runner: func(args []string, script string) ([]byte, error) {
		value, err := hostRequest(ctx, "actionlint/runTool", struct {
			Name  string   `json:"name"`
			Args  []string `json:"args"`
			Stdin string   `json:"stdin"`
		}{name, args, script})
		if err != nil {
			return nil, err
		}
		if value.Type() != js.TypeString {
			return nil, errors.New("tool response must be text")
		}
		return []byte(value.String()), nil
	}}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, input := io.Pipe()
	output, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	queue := make(chan string, 128)
	receive := js.FuncOf(func(_ js.Value, args []js.Value) any {
		select {
		case queue <- args[0].String():
			return true
		default:
			return false
		}
	})
	defer receive.Release()
	js.Global().Set("actionlintReceiveMessage", receive)
	go func() {
		defer input.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case text := <-queue:
				if _, err := fmt.Fprintf(input, "Content-Length: %d\r\n\r\n%s", len(text), text); err != nil {
					return
				}
			}
		}
	}()
	go func() {
		reader := bufio.NewReader(output)
		for {
			message, err := langserver.ReadMessage(reader)
			if err != nil {
				return
			}
			js.Global().Call("actionlintSendMessage", string(message))
		}
	}()
	js.Global().Call("actionlintReady")
	if err := serve(ctx, in, out); err != nil && !errors.Is(err, context.Canceled) {
		js.Global().Call("actionlintServerError", err.Error())
	}
}
