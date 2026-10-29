package langserver

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
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	sourceposition "actionlint.kjanat.dev/v2/internal/source/position"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	sourcemodel "actionlint.kjanat.dev/v2/source"
)

type Document struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

// Analysis supplies the captured source text used for diagnostic coordinates.
// Sources and Diagnostics must belong to the same analysis generation.
type Analysis struct {
	Diagnostics []diagnostic.Diagnostic
	Sources     map[string]Source
}

// Source carries the sealed revision and coordinate model used for publication.
type Source struct {
	Text        string
	Revision    sourcemodel.Revision
	Coordinates sourcemodel.Coordinates
}

func (s Source) valid() bool {
	return s.Coordinates == sourcemodel.YAML && s.Revision.Valid() && utf8.ValidString(s.Text) && s.Revision.Digest() == sourceid.Digest([]byte(s.Text))
}

type (
	Analyze        func(context.Context, map[string]Document) (Analysis, error)
	analysisResult struct {
		generation int
		analysis   Analysis
		err        error
	}
)

type incoming struct {
	message message
	err     error
}

func analyzeGeneration(ctx context.Context, analyze Analyze, documents map[string]Document, generation int, results chan<- analysisResult) {
	analysis, err := analyze(ctx, documents)
	select {
	case results <- analysisResult{generation, analysis, err}:
	case <-ctx.Done():
	}
}

func currentAnalysis(result analysisResult, generation int, shutdown bool) (Analysis, error, bool) {
	if result.generation != generation || shutdown {
		return Analysis{}, nil, false
	}
	return result.analysis, result.err, true
}

// Serve uses full or ranged document updates, cancels superseded analyses, and
// republishes all open dependants after changes. Analyze must honor cancellation.
// When input is closable, Serve closes it on exit to interrupt a pending read.
func Serve(ctx context.Context, in io.Reader, out io.Writer, analyze Analyze, completions ...Complete) error {
	ctx, cancel := context.WithCancel(ctx)

	input := make(chan incoming, 1)
	go func() {
		r := bufio.NewReader(in)
		for {
			m, err := readMessage(r)
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
	latest := map[string][]diagnostic.Diagnostic{}
	published := map[string]Document{}
	publishedSources := map[string]Document{}
	results := make(chan analysisResult, 1)
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
	respond := func(id json.RawMessage, result any, rpcErr any) error {
		v := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			v["error"] = rpcErr
		} else {
			v["result"] = result
		}
		return writeMessage(out, v)
	}
	notify := func(method string, params any) error {
		return writeMessage(out, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	schedule := func() {
		latest = map[string][]diagnostic.Diagnostic{}
		publishedSources = map[string]Document{}
		stop()
		generation++
		snapshot := maps.Clone(documents)
		gen := generation
		var work context.Context
		work, stop = context.WithCancel(ctx)
		workers.Go(func() {
			analyzeGeneration(work, analyze, snapshot, gen, results)
		})
	}
serve:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-results:
			analysis, analysisErr, currentGeneration := currentAnalysis(result, generation, shutdown)
			if !currentGeneration {
				continue
			}
			latest = map[string][]diagnostic.Diagnostic{}
			// Editor metadata supplies URI/version; captured text supplies ranges.
			sources := make(map[string]Document, len(analysis.Sources))
			for path, captured := range analysis.Sources {
				if !captured.valid() {
					continue
				}
				doc, open := documents[path]
				if !open {
					doc.URI = FileURI(path)
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
				keptRelated := make([]diagnostic.Related, 0, len(d.Related))
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
					if _, _, err := sourceposition.Range([]byte(target.Text), row.Coordinates, related.Start, related.End); err != nil {
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
					doc = Document{URI: FileURI(path)}
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
				initialized = true
				capabilities := map[string]any{"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 2}, "codeActionProvider": true}
				if len(completions) > 0 {
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
			case "shutdown":
				shutdown = true
				stop()
				if err := respond(m.ID, nil, nil); err != nil {
					return err
				}
			case "initialized", "$/cancelRequest":
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
				path, err := filePath(p.TextDocument.URI)
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
						Range *sourceRange `json:"range"`
						Text  string       `json:"text"`
					} `json:"contentChanges"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := filePath(p.TextDocument.URI)
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
					start, err := byteOffset(doc.Text, change.Range.Start)
					if err != nil {
						if err := reject(err); err != nil {
							return err
						}
						continue serve
					}
					end, err := byteOffset(doc.Text, change.Range.End)
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
				path, err := filePath(p.TextDocument.URI)
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
					TextDocument Document `json:"textDocument"`
					Position     position `json:"position"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := filePath(p.TextDocument.URI)
				if err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				items := []completionItem{}
				if doc, ok := documents[path]; ok && len(completions) > 0 {
					offset, err := byteOffset(doc.Text, p.Position)
					if err != nil {
						if err := reject(err); err != nil {
							return err
						}
						continue serve
					}
					for _, item := range completions[0](ctx, doc, offset) {
						items = append(items, completionItemFor(doc.Text, item))
					}
				}
				if err := respond(m.ID, map[string]any{"isIncomplete": false, "items": items}, nil); err != nil {
					return err
				}
			case "textDocument/codeAction":
				var p struct {
					TextDocument Document    `json:"textDocument"`
					Range        sourceRange `json:"range"`
				}
				if err := jsonv2.Unmarshal(m.Params, &p); err != nil {
					if err := reject(err); err != nil {
						return err
					}
					continue serve
				}
				path, err := filePath(p.TextDocument.URI)
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
