package bundled_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/itchyny/gojq"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
)

var update = flag.Bool("update", false, "rewrite testdata/*/diagnostics.json from the decoders")

func TestBundledManifestsValidate(t *testing.T) {
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, manifest := range manifests {
		names = append(names, manifest.Rule.Name)
		if manifest.Selectors.Filename == "" {
			t.Errorf("%s: bundled script providers name their virtual snippet file", manifest.Rule.Name)
		}
	}
	if want := []string{"ruff", "shellcheck"}; !slices.Equal(names, want) {
		t.Fatalf("bundled providers = %v, want %v", names, want)
	}
}

// Every program file is referenced by its manifest, so an edited program
// without an updated digest fails here with the digest to record.
func TestBundledProgramsAreReferenced(t *testing.T) {
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	referenced := map[string]bool{}
	for _, manifest := range manifests {
		for _, transform := range []*provider.Transform{manifest.Process.Config.Transform, manifest.Process.Output.Transform} {
			if transform != nil {
				referenced[transform.Digest] = true
			}
		}
	}
	err = fs.WalkDir(bundled.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(name, ".jq") {
			return err
		}
		data, err := fs.ReadFile(bundled.FS(), name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if digest := "sha256:" + hex.EncodeToString(sum[:]); !referenced[digest] {
			t.Errorf("%s is not referenced by its manifest; its digest is %s", name, digest)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Golden decoding: testdata/<case>/stdout.json decoded with vars.json must
// equal diagnostics.json.
//
// Provenance: shellcheck/script is ShellCheck 0.11.0 `-f json1 --shell bash
// -e SC1091,SC2194,SC2050,SC2153,SC2154,SC2157,SC2043 -`; ruff/lint and
// ruff/syntax are Ruff 0.17.0 `check --isolated --ignore-noqa --no-fix
// --no-cache --output-format json --stdin-filename actionlint.py --extension
// py:python ... -`. shellcheck/sourced and ruff/preview are hand-written:
// ShellCheck 0.11.0 never reports findings in followed files or a sources
// list, and Ruff 0.17.0 no longer emits null codes, but the native adapters
// accept both (internal/application/shellcheck_sources_test.go,
// internal/tools/ruff/diagnostic_test.go).
func TestBundledGoldenDecode(t *testing.T) {
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		decoder := outputDecoder(t, manifest)
		cases, err := filepath.Glob(filepath.Join(manifest.Rule.Name, "testdata", "*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(cases) == 0 {
			t.Errorf("%s has no golden decode cases", manifest.Rule.Name)
		}
		for _, dir := range cases {
			t.Run(filepath.ToSlash(dir), func(t *testing.T) {
				stdout := readFile(t, filepath.Join(dir, "stdout.json"))
				var vars map[string]string
				if err := jsonv2.Unmarshal(readFile(t, filepath.Join(dir, "vars.json")), &vars); err != nil {
					t.Fatal(err)
				}
				result, err := decoder.Decode(t.Context(), stdout, vars)
				if err != nil {
					t.Fatal(err)
				}
				got, err := jsonv2.Marshal(result, jsontext.WithIndent("  "))
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, '\n')
				golden := filepath.Join(dir, "diagnostics.json")
				if *update {
					if err := os.WriteFile(golden, got, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if want := readFile(t, golden); !bytes.Equal(got, want) {
					t.Errorf("decoded diagnostics differ from %s (-want +got):\n%s", golden, cmp.Diff(string(want), string(got)))
				}
			})
		}
	}
}

func TestRuffConfigTransform(t *testing.T) {
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == "ruff" })
	if index < 0 {
		t.Fatal("ruff is not bundled")
	}
	program, ok := bundled.Program(manifests[index].Process.Config.Transform.Digest)
	if !ok {
		t.Fatal("ruff config transform is not bundled")
	}
	query, err := gojq.Parse(string(program))
	if err != nil {
		t.Fatal(err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, input, want string }{
		{"absent", `null`, `{"config":{},"vars":{}}`},
		{"native options pass through", `{"line-length":100,"lint":{"extend-select":["B"]}}`, `{"config":{"line-length":100,"lint":{"extend-select":["B"]}},"vars":{}}`},
		{"top-level native selection passes through", `{"select":["E"],"lint":{"select":["F"]}}`, `{"config":{"select":["E"]},"vars":{"select":"F"}}`},
		{"selection becomes flags", `{"lint":{"select":["F","E501"],"ignore":[],"per-file-ignores":{}}}`, `{"config":{"lint":{"per-file-ignores":{}}},"vars":{"select":"F,E501"}}`},
		{"ignore without selection", `{"lint":{"ignore":["F401"]}}`, `{"config":{},"vars":{"ignore":"F401"}}`},
		{"empty selection disables lint codes", `{"lint":{"select":[],"ignore":["E501"]}}`, `{"config":{},"vars":{"ignore":"ALL","select":"ALL"}}`},
		{"null selection is unset", `{"lint":{"select":null}}`, `{"config":{},"vars":{}}`},
		{"configured target replaces the inferred one", `{"target-version":"py311"}`, `{"config":{},"vars":{"target_version":"py311"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input any
			if err := jsonv2.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}
			iter := code.RunWithContext(t.Context(), input)
			value, ok := iter.Next()
			if !ok {
				t.Fatal("no output")
			}
			if err, failed := value.(error); failed {
				t.Fatal(err)
			}
			got, err := gojq.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
			if _, more := iter.Next(); more {
				t.Error("config transform produced more than one value")
			}
		})
	}
}

// outputDecoder builds the decoder a manifest's output declares. Stage 2 owns
// this mapping in the process transport; the test needs only the jq route.
func outputDecoder(t *testing.T, manifest provider.Manifest) process.Decoder {
	t.Helper()
	output := manifest.Process.Output
	if output.Format != provider.FormatJSON || output.Transform == nil {
		t.Fatalf("%s: bundled providers decode JSON with a transform, got %q", manifest.Rule.Name, output.Format)
	}
	program, ok := bundled.Program(output.Transform.Digest)
	if !ok {
		t.Fatalf("%s: missing output program", manifest.Rule.Name)
	}
	decoder, err := process.NewJQ(string(program), process.JQJSON)
	if err != nil {
		t.Fatalf("%s: %v", manifest.Rule.Name, err)
	}
	return decoder
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
