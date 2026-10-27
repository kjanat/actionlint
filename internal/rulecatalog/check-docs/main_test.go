package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var playgroundLinkRe = regexp.MustCompile(`\[Playground\]\(https://kjanat\.github\.io/actionlint/#([A-Za-z0-9+/=]+)\)`)

// compress/flate output differs across Go releases.
func decodePlaygroundLinks(t *testing.T, doc []byte) []byte {
	t.Helper()
	return playgroundLinkRe.ReplaceAllFunc(doc, func(m []byte) []byte {
		enc := playgroundLinkRe.FindSubmatch(m)[1]
		raw, err := base64.StdEncoding.DecodeString(string(enc))
		if err != nil {
			t.Fatal(err)
		}
		r, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		dec, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte("[Playground]("), append(dec, ')')...)
	})
}

// Do not mess up the test output. Temporarily commenting out this function may be helpful when debugging some test cases.
func init() {
	log.SetOutput(io.Discard)
	stderr = io.Discard
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func testErr(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("error message %q does not contain expected text %q", msg, w)
		}
	}
}

func TestActionlintOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "expression",
			src: `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ unknown }}
`,
			want: `test.yaml:6:23: undefined variable "unknown"`,
		},
		{
			name: "local reusable workflow",
			src: `on: push
jobs:
  test:
    uses: ./.github/workflows/not-existing-workflow.yml
`,
			want: `could not read reusable workflow file for "./.github/workflows/not-existing-workflow.yml": open /path/to/repo`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Actionlint([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out, []byte(tc.want)) {
				t.Fatalf("expected diagnostic containing %q outside a Git checkout, got:\n%s", tc.want, out)
			}
		})
	}
}

func TestMainGenerateOK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check-checks doesn't support Windows")
	}
	root := t.TempDir()

	in := must(os.Open(filepath.FromSlash("testdata/ok/minimal.in")))
	path := filepath.FromSlash(root + "/minimal.in")
	tmp := must(os.Create(path))
	must(io.Copy(tmp, in))
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Main([]string{"exe", "-fix", path}); err != nil {
		t.Fatal(err)
	}

	want := must(os.ReadFile(filepath.FromSlash("testdata/ok/minimal.out")))
	have := must(os.ReadFile(path))
	if diff := cmp.Diff(decodePlaygroundLinks(t, want), decodePlaygroundLinks(t, have)); diff != "" {
		t.Fatal(diff)
	}
}

func TestMainCheckOK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check-checks doesn't support Windows")
	}
	path := filepath.FromSlash("testdata/ok/minimal.out")
	if err := Main([]string{"exe", path}); err != nil {
		t.Fatal(err)
	}
}

func TestMainCheckQuietOK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check-checks doesn't support Windows")
	}
	path := filepath.FromSlash("testdata/ok/minimal.out")
	if err := Main([]string{"exe", "-quiet", path}); err != nil {
		t.Fatal(err)
	}
}

func TestMainPrintHelp(t *testing.T) {
	if err := Main([]string{"exe", "-help"}); err != nil {
		t.Fatal(err)
	}
}

func TestActionlintMissingLinters(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Actionlint([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`))
	testErr(t, err, "missing from PATH: shellcheck", "runner lint-docs")
}

func TestMainCheckError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check-checks doesn't support Windows")
	}
	path := filepath.FromSlash("testdata/ok/minimal.in")
	testErr(t, Main([]string{"exe", path}), "checks document has some update")
}

func TestMainFileNotFound(t *testing.T) {
	testErr(t, Main([]string{"exe", "this-file-does-not-exist.md"}), "could not read the document file")
}

func TestMainTooManyArgs(t *testing.T) {
	testErr(t, Main([]string{"exe", "a", "b", "c"}), "this command should take exact one file path but got")
}

func TestMainInvalidCheckFlag(t *testing.T) {
	testErr(t, Main([]string{"exe", "-c", "foo.md"}), "flag provided but not defined")
}

func TestMainUpdateError(t *testing.T) {
	path := filepath.FromSlash("testdata/err/no_playground_link.md")
	if err := Main([]string{"exe", path}); err == nil {
		t.Fatal("no error occurred")
	}
}

func TestUpdateOK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check-checks doesn't support Windows")
	}

	dir := filepath.FromSlash("testdata/ok")

	tests := []string{}
	for _, e := range must(os.ReadDir(dir)) {
		n := e.Name()
		if !strings.HasSuffix(n, ".in") {
			continue
		}
		tests = append(tests, strings.TrimSuffix(n, filepath.Ext(n)))
	}

	for _, tc := range tests {
		in := filepath.Join(dir, tc+".in")
		out := filepath.Join(dir, tc+".out")
		t.Run(tc, func(t *testing.T) {
			have, err := Update(must(os.ReadFile(in)))
			if err != nil {
				t.Fatal(err)
			}
			want := must(os.ReadFile(out))
			if diff := cmp.Diff(decodePlaygroundLinks(t, want), decodePlaygroundLinks(t, have)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestUpdateError(t *testing.T) {
	dir := filepath.FromSlash("testdata/err")

	tests := []string{}
	for _, e := range must(os.ReadDir(dir)) {
		n := e.Name()
		if !strings.HasSuffix(n, ".md") {
			continue
		}
		tests = append(tests, strings.TrimSuffix(n, filepath.Ext(n)))
	}

	for _, tc := range tests {
		path := filepath.Join(dir, tc+".md")
		t.Run(tc, func(t *testing.T) {
			if _, err := Update(must(os.ReadFile(path))); err == nil {
				t.Fatal("no error occurred")
			}
		})
	}
}

func TestPlaygroundLinkSizeLimit(t *testing.T) {
	line := append(bytes.Repeat([]byte("a"), 1023), '\n')
	u := NewUpdater(nil)
	u.PlaygroundLink(bytes.Repeat(line, playgroundMaxSourceBytes/len(line)))
	if u.firstErr != nil {
		t.Fatalf("a source at the limit failed: %v", u.firstErr)
	}
	u.PlaygroundLink(bytes.Repeat(line, playgroundMaxSourceBytes/len(line)+1))
	if u.firstErr == nil || !strings.Contains(u.firstErr.Error(), "larger than the playground") {
		t.Fatalf("a source past the limit was accepted: %v", u.firstErr)
	}
}

func TestPlaygroundLinkURLLimit(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	var src bytes.Buffer
	for src.Len() < 2*playgroundMaxURLLength {
		for range 1023 {
			src.WriteByte(byte('a' + random.IntN(26)))
		}
		src.WriteByte('\n')
	}
	u := NewUpdater(nil)
	u.PlaygroundLink(src.Bytes())
	if u.firstErr == nil || !strings.Contains(u.firstErr.Error(), "browsers load") {
		t.Fatalf("an incompressible source produced a link past the URL limit: %v", u.firstErr)
	}
}

func TestPlaygroundLinkTrailingData(t *testing.T) {
	link := NewUpdater(nil).PlaygroundLink([]byte("on: push"))
	if payload, ok := playgroundLinkPayload(link); !ok || string(payload) != "on: push" {
		t.Fatalf("a generated link does not decode: %q, %v", payload, ok)
	}
	enc := strings.TrimSuffix(strings.TrimPrefix(link, "[Playground]("+playgroundURL), ")")
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	padded := "[Playground](" + playgroundURL + base64.StdEncoding.EncodeToString(append(raw, 0, 1, 2)) + ")"
	if _, ok := playgroundLinkPayload(padded); ok {
		t.Fatal("a link with bytes after its zlib stream decoded")
	}
}

func TestPlaygroundLinkLimits(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), playgroundMaxSourceBytes+1)
	if err := playgroundLinkError(payload, "[Playground]("+playgroundURL+"eJw=)"); err == nil {
		t.Fatal("a link with an oversized payload passed")
	}
	if err := playgroundLinkError([]byte("on: push"), "[Playground]("+playgroundURL+strings.Repeat("A", playgroundMaxURLLength)+")"); err == nil {
		t.Fatal("a link past the URL limit passed")
	}
	if err := playgroundLinkError([]byte("on: push"), "[Playground]("+playgroundURL+"eJw=)"); err != nil {
		t.Fatalf("a small link failed: %v", err)
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state state
		want  string
	}{
		{stateInit, "init"},
		{stateAnchor, "anchor"},
		{stateHeading, "heading"},
		{stateInputHeader, "input header"},
		{stateInputBlock, "input block"},
		{stateAfterInput, "after input"},
		{stateOutputHeader, "output header"},
		{stateAfterOutput, "after output"},
		{stateOutputBlock, "output block"},
		{stateEnd, "end"},
	}

	for _, tc := range tests {
		have := tc.state.String()
		if have != tc.want {
			t.Errorf("wanted %q for state %d but have %q", tc.want, tc.state, have)
		}
	}
}
