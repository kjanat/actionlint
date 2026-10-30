package local

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
)

func TestMetadataDocumentRetainsSameLookupProvenance(t *testing.T) {
	for _, resolver := range []bool{false, true} {
		for _, broken := range []bool{false, true} {
			name := "cache"
			if resolver {
				name = "mapped-resolver"
			}
			if broken {
				name += "/parse-failure"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				content := "name: captured\ndescription: provenance\nruns: {using: composite, steps: []}\n"
				if broken {
					content = "name: [\n"
				}
				var reads atomic.Int32
				cache := newCapturedActionCache(t, root, nil)
				cache.Configure(Options{CustomReader: true, Reader: fsread.New(t.Context(), func(string) ([]byte, error) {
					reads.Add(1)
					return []byte(content), nil
				}, nil)})
				lookup := cache.FindMetadataDocument
				spec := "./source"
				if resolver {
					view := NewResolver(cache, map[string]string{"./generated": "./source"})
					lookup = view.FindMetadataDocument
					spec = "./generated"
				}
				metadata, cached, document, err := lookup(spec)
				if cached || (err != nil) != broken || (metadata == nil) != broken {
					t.Fatalf("first lookup: metadata=%+v cached=%v error=%v", metadata, cached, err)
				}
				if broken {
					if _, ok := errors.AsType[*ParseError](err); !ok || document.Provenance() == (documentparse.Provenance{}) {
						t.Fatalf("failed parse lost original provenance: %v", err)
					}
				} else if original, parsed := metadata.OriginalSource(); metadata.SourceMapped != resolver || !parsed || original != string(document.Content()) {
					t.Fatal("returned metadata no longer matches its captured parse")
				}
				if !document.Capture().Valid() || string(document.Content()) != content || document.Address() != filepath.Join(root, "source", "action.yml") {
					t.Fatalf("lookup source does not match capture: %+v", document)
				}
				_, cached, again, err := lookup(spec)
				if !cached || err != nil || again.Capture().Revision() != document.Capture().Revision() || reads.Load() != 1 {
					t.Fatalf("cached lookup reread or re-reported failure: cached=%v error=%v reads=%d", cached, err, reads.Load())
				}
			})
		}
	}
}
