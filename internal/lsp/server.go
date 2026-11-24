package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/byteoffset"
	"actionlint.kjanat.dev/v2/internal/results"
)

type incoming struct {
	message RPCMessage
	err     error
}

// Serve requires Analyze to honor cancellation and closes a closable input on exit.
func Serve(ctx context.Context, in io.Reader, out io.Writer, analyze Analyze, completions ...Complete) error {
	options := Options{Analyze: analyze}
	if len(completions) > 0 {
		options.Complete = completions[0]
	}
	return ServeWithOptions(ctx, in, out, options)
}

// Options with nil URI converters use local file URIs. Initialize runs before
// any document request.
type Options struct {
	Discover    func(context.Context, []string, bool) ([]string, error)
	Analyze     Analyze
	Complete    Complete
	Initialize  func(json.RawMessage) error
	PathFromURI func(string) (string, error)
	URIFromPath func(string) string
}

func ServeWithOptions(ctx context.Context, in io.Reader, out io.Writer, options Options) error {
	analyze := options.Analyze
	if analyze == nil {
		return errors.New("language server requires an analysis function")
	}
	pathFromURI := options.PathFromURI
	if pathFromURI == nil {
		pathFromURI = filePath
	}
	uriFromPath := options.URIFromPath
	if uriFromPath == nil {
		uriFromPath = sourcetext.FileURI
	}
	ctx, cancel := context.WithCancel(ctx)

	input := make(chan incoming, 1)
	go func() {
		r := bufio.NewReader(in)
		for {
			m, err := ReadRPCMessage(r)
			select {
			case input <- incoming{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	documents := map[string]Document{}
	latest := map[string][]results.Diagnostic{}
	published := map[string]Document{}
	publishedSources := map[string]Document{}
	completedAnalyses := make(chan analysisResult, 1)
	type discoveryResult struct {
		id    json.RawMessage
		paths []string
		err   error
	}
	completedDiscoveries := make(chan discoveryResult)
	discoveries := make(map[string]context.CancelFunc)
	generation := 0
	stop := func() {}
	var workers sync.WaitGroup
	defer func() {
		cancel()
		stop()
		if closer, ok := in.(io.Closer); ok {
			_ = closer.Close()
		}
		workers.Wait()
	}()
	initialized, shutdown := false, false
	respond := func(id json.RawMessage, result, rpcErr any) error {
		v := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			v["error"] = rpcErr
		} else {
			v["result"] = result
		}
		return WriteRPCMessage(out, v)
	}
	notify := func(method string, params any) error {
		return WriteRPCMessage(out, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	schedule := func() {
		latest = map[string][]results.Diagnostic{}
		publishedSources = map[string]Document{}
		stop()
		generation++
		snapshot := maps.Clone(documents)
		gen := generation
		var work context.Context
		work, stop = context.WithCancel(ctx)
		workers.Go(func() {
			analyzeGeneration(work, analyze, snapshot, gen, completedAnalyses)
		})
	}
serve:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-completedDiscoveries:
			key := string(result.id)
			discoveries[key]()
			delete(discoveries, key)
			var rpcErr any
			if errors.Is(result.err, context.Canceled) || shutdown {
				rpcErr = map[string]any{"code": -32800, "message": "discovery cancelled"}
			} else if result.err != nil {
				rpcErr = map[string]any{"code": -32602, "message": result.err.Error()}
			}
			if err := respond(result.id, result.paths, rpcErr); err != nil {
				return err
			}
		case result := <-completedAnalyses:
			analysis, analysisErr, currentGeneration := currentAnalysis(result, generation, shutdown)
			if !currentGeneration {
				continue
			}
			latest = map[string][]results.Diagnostic{}
			sources := make(map[string]Document, len(analysis.Sources))
			for path, captured := range analysis.Sources {
				if !captured.valid() {
					continue
				}
				doc, open := documents[path]
				if !open {
					doc.URI = uriFromPath(path)
				}
				doc.Text = captured.Text
				sources[path] = doc
			}
			missingSources := make(map[string]struct{})
			for _, d := range analysis.Diagnostics {
				captured, available := sources[d.Path]
				if !available {
					missingSources[d.Path] = struct{}{}
					continue
				}
				row := analysis.Sources[d.Path]
				if d.Reference != nil && (d.Reference.Document != row.Revision.Document() || d.Reference.Revision != row.Revision.Digest()) {
					missingSources[d.Path] = struct{}{}
					continue
				}
				if _, err := diagnosticRange([]byte(captured.Text), d.Start, d.End); err != nil {
					missingSources[d.Path] = struct{}{}
					continue
				}
				keptRelated := make([]results.Related, 0, len(d.Related))
				for _, related := range d.Related {
					target, available := sources[related.Path]
					if !available {
						missingSources[related.Path] = struct{}{}
						continue
					}
					row := analysis.Sources[related.Path]
					if !related.Revision.Valid() || related.Revision != row.Revision || related.Coordinates != row.Coordinates {
						missingSources[related.Path] = struct{}{}
						continue
					}
					if _, _, err := byteoffset.Range([]byte(target.Text), row.Coordinates, related.Start, related.End); err != nil {
						missingSources[related.Path] = struct{}{}
						continue
					}
					keptRelated = append(keptRelated, related)
				}
				d.Related = keptRelated
				latest[d.Path] = append(latest[d.Path], d)
			}
			if len(missingSources) != 0 {
				analysisErr = errors.Join(analysisErr, fmt.Errorf("incomplete analysis: diagnostic sources unavailable for %d paths: %s", len(missingSources), strings.Join(slices.Sorted(maps.Keys(missingSources)), ", ")))
			}
			if analysisErr != nil {
				if err := notify("window/logMessage", map[string]any{"type": 1, "message": analysisErr.Error()}); err != nil {
					return err
				}
			}
			current := maps.Clone(documents)
			for path := range latest {
				doc, ok := sources[path]
				if !ok {
					doc = Document{URI: uriFromPath(path)}
				}
				current[path] = doc
			}
			// Include old publications once more to clear removed dependencies.
			targets := maps.Clone(published)
			maps.Copy(targets, current)
			for _, path := range slices.Sorted(maps.Keys(targets)) {
				doc := targets[path]
				items := diagnosticItems(doc, latest[path], sources)
				params := map[string]any{"uri": doc.URI, "diagnostics": items}
				if open, ok := documents[path]; ok {
					params["version"] = open.Version
				}
				if err := notify("textDocument/publishDiagnostics", params); err != nil {
					return err
				}
			}
			published = current
			publishedSources = sources
		case event := <-input:
			if event.err != nil {
				if errors.Is(event.err, io.EOF) {
					return nil
				}
				return event.err
			}
			m := event.message
			reject := func(err error) error {
				if len(m.ID) > 0 {
					return respond(m.ID, nil, map[string]any{"code": -32602, "message": err.Error()})
				}
				return notify("window/logMessage", map[string]any{"type": 1, "message": err.Error()})
			}
			if m.Method == "exit" {
				if !shutdown {
					return errors.New("exit before shutdown")
				}
				return nil
			}
			if m.Method == "initialize" && !initialized {
				if options.Initialize != nil {
					if err := options.Initialize(m.Params); err != nil {
						if err := reject(err); err != nil {
							return err
						}
						continue serve
					}
				}
				initialized = true
				capabilities := map[string]any{"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 2}, "codeActionProvider": true}
				if options.Discover != nil {
					capabilities["experimental"] = map[string]any{"actionlintDiscover": true}
				}
				if options.Complete != nil {
					capabilities["completionProvider"] = map[string]any{"triggerCharacters": []string{".", "[", "'"}}
				}
				if err := respond(m.ID, map[string]any{"capabilities": capabilities, "serverInfo": map[string]string{"name": "actionlint"}}, nil); err != nil {
					return err
				}
				continue
			}
			if !initialized || shutdown {
				if len(m.ID) > 0 {
					if err := respond(m.ID, nil, map[string]any{"code": -32002, "message": "server is not running"}); err != nil {
						return err
					}
				}
				continue
			}
			switch m.Method {
			case "actionlint/discover":
				if len(m.ID) == 0 {
					continue serve
				}
				if options.Discover == nil {
					if err := respond(m.ID, nil, map[string]any{"code": -32601, "message": "discovery is not available in this host"}); err != nil {
						return err
					}
					continue serve
				}
				var params struct {
					Paths    []string `json:"paths"`
					NoIgnore bool     `json:"noIgnore"`
				}
				if err := jsonv2.Unmarshal(m.Params, &params); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				key := string(m.ID)
				if _, pending := discoveries[key]; pending {
					if err := reject(errors.New("discovery request ID is already in use")); err != nil {
						return err
					}
					continue serve
				}
				work, cancelDiscovery := context.WithCancel(ctx)
				discoveries[key] = cancelDiscovery
				workers.Go(func() {
					paths, err := options.Discover(work, params.Paths, params.NoIgnore)
					if work.Err() != nil {
						err = work.Err()
					}
					select {
					case completedDiscoveries <- discoveryResult{m.ID, paths, err}:
					case <-ctx.Done():
					}
				})
			case "shutdown":
				shutdown = true
				stop()
				for _, cancelDiscovery := range discoveries {
					cancelDiscovery()
				}
				if err := respond(m.ID, nil, nil); err != nil {
					return err
				}
			case "$/cancelRequest":
				var params struct {
					ID json.RawMessage `json:"id"`
				}
				if jsonv2.Unmarshal(m.Params, &params) == nil {
					if cancelDiscovery := discoveries[string(params.ID)]; cancelDiscovery != nil {
						cancelDiscovery()
					}
				}
			case "initialized":
			case "textDocument/didOpen":
				var p struct {
					TextDocument Document `json:"textDocument"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := pathFromURI(p.TextDocument.URI)
				if err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				documents[path] = p.TextDocument
				schedule()
			case "textDocument/didChange":
				var p struct {
					TextDocument   Document `json:"textDocument"`
					ContentChanges []struct {
						Range *editorRange `json:"range"`
						Text  string       `json:"text"`
					} `json:"contentChanges"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := pathFromURI(p.TextDocument.URI)
				if err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				doc, ok := documents[path]
				if !ok || p.TextDocument.Version <= doc.Version {
					continue
				}
				for _, change := range p.ContentChanges {
					if change.Range == nil {
						doc.Text = change.Text
						continue
					}
					start, err := editorByteOffset(doc.Text, change.Range.Start)
					if err != nil {
						if err := reject(err); err != nil {
							return err
						}
						continue serve
					}
					end, err := editorByteOffset(doc.Text, change.Range.End)
					if err != nil || end < start {
						if err := reject(errors.New("invalid document change range")); err != nil {
							return err
						}
						continue serve
					}
					doc.Text = doc.Text[:start] + change.Text + doc.Text[end:]
				}
				doc.Version = p.TextDocument.Version
				documents[path] = doc
				delete(latest, path)
				schedule()
			case "textDocument/didClose":
				var p struct {
					TextDocument Document `json:"textDocument"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := pathFromURI(p.TextDocument.URI)
				if err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				delete(documents, path)
				delete(latest, path)
				delete(published, path)
				if err := notify("textDocument/publishDiagnostics", map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{}}); err != nil {
					return err
				}
				schedule()
			case "textDocument/completion":
				var p struct {
					TextDocument Document       `json:"textDocument"`
					Position     editorPosition `json:"position"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := pathFromURI(p.TextDocument.URI)
				if err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				items := []completionItem{}
				if doc, ok := documents[path]; ok && options.Complete != nil {
					offset, err := editorByteOffset(doc.Text, p.Position)
					if err != nil {
						if err := reject(err); err != nil {
							return err
						}
						continue serve
					}
					for _, item := range options.Complete(ctx, doc, offset) {
						items = append(items, completionItemFor(doc.Text, item))
					}
				}
				if err := respond(m.ID, map[string]any{"isIncomplete": false, "items": items}, nil); err != nil {
					return err
				}
			case "textDocument/codeAction":
				var p struct {
					TextDocument Document    `json:"textDocument"`
					Range        editorRange `json:"range"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := pathFromURI(p.TextDocument.URI)
				if err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				actions := codeActions(ctx, path, p.Range, latest[path], publishedSources, documents)
				if err := respond(m.ID, actions, nil); err != nil {
					return err
				}
			default:
				if len(m.ID) > 0 {
					if err := respond(m.ID, nil, map[string]any{"code": -32601, "message": "method not supported"}); err != nil {
						return err
					}
				}
			}
		}
	}
}
