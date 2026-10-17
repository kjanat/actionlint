package application_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/render"
	"actionlint.kjanat.dev/internal/workspace"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowDiagnosticTemplates(t *testing.T) {
	tests := []struct {
		file   string
		format string
	}{
		{
			file:   "test.json",
			format: "{{json .}}",
		},
		{
			file:   "test.jsonl",
			format: "{{range $err := .}}{{json $err}}{{end}}",
		},
		{
			file:   "test.md",
			format: "{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\\n\\n{{$.Message}}\\n\\n```\\n{{$.Snippet}}\\n```\\n\\n{{end}}",
		},
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("testdata", "format")
	proj := workspace.Identity(filepath.Join(root, dir))
	infile := filepath.Join(dir, "test.yaml")
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			var b strings.Builder
			content, err := os.ReadFile(filepath.Join(root, infile))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: infile, InputPath: filepath.Join(root, infile), Content: content, Project: proj, Config: &repoconfig.Config{}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) == 0 {
				t.Fatal("no diagnostics")
			}
			renderer, err := render.New("", tc.format, false, func() string { return "test" }, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := renderer.Render(&b, render.Prepare(*result)); err != nil {
				t.Fatal(err)
			}
			buf, err := os.ReadFile(filepath.Join(root, dir, tc.file))
			if err != nil {
				panic(err)
			}
			want := string(buf)

			have := b.String()
			// Fix path separators on Windows
			if runtime.GOOS == "windows" {
				slash := filepath.ToSlash(infile)
				have = strings.ReplaceAll(have, infile, slash)
				escaped := strings.ReplaceAll(slash, "/", `\\`)
				have = strings.ReplaceAll(have, escaped, slash)
			}

			if diff := cmp.Diff(want, have); diff != "" {
				t.Logf("have: %s", have)
				t.Fatal(diff)
			}
		})
	}
}
func TestWorkflowDiagnosticSARIF(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("testdata", "format")
	proj := workspace.Identity(filepath.Join(root, dir))
	file := filepath.Join(dir, "test.yaml")

	var b strings.Builder
	content, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: file, InputPath: filepath.Join(root, file), Content: content, Project: proj, Config: &repoconfig.Config{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("no diagnostics")
	}
	renderer, err := render.New(render.FormatSARIF, "", false, func() string { return "(devel)" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderer.Render(&b, render.Prepare(*result)); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// Fix path separators on Windows
	if runtime.GOOS == "windows" {
		slash := filepath.ToSlash(file)
		escaped := strings.ReplaceAll(file, `\`, `\\`)
		out = strings.ReplaceAll(out, escaped, slash)
	}

	var have any
	if err := json.Unmarshal([]byte(out), &have); err != nil {
		t.Fatalf("output is not JSON: %v: %q", err, out)
	}

	bytes, err := os.ReadFile(filepath.Join(root, dir, "test.sarif"))
	if err != nil {
		panic(err)
	}
	var want any
	if err := json.Unmarshal(bytes, &want); err != nil {
		panic(err)
	}

	if diff := cmp.Diff(want, have); diff != "" {
		t.Logf("have: %s", have)
		t.Fatal(diff)
	}
}
