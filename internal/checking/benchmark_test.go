package checking_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	render "actionlint.kjanat.dev/v2/internal/output"

	"golang.org/x/sys/execabs"
)

func BenchmarkLintWorkflowFiles(b *testing.B) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		b.Fatal(err)
	}

	scripts := filepath.Join(root, "internal", "development", "tests", "integration", "workflow", "testdata", "bench", "many_scripts.yaml")
	small := filepath.Join(root, "internal", "development", "tests", "integration", "workflow", "testdata", "bench", "small.yaml")
	large := filepath.Join(root, "internal", "development", "tests", "integration", "workflow", "testdata", "bench", "large.yaml")
	minimal := filepath.Join(root, "internal", "development", "tests", "integration", "workflow", "testdata", "bench", "minimal.yaml")
	proj := workspace.Identity(root)
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		b.Skipf("shellcheck is not found: %s", err)
	}
	format := "{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\\n\\n{{$.Message}}\\n\\n```\\n{{$.Snippet}}\\n```\\n\\n{{end}}"

	// Workflow files for this repository
	ours := []string{}
	{
		d := filepath.Join(root, ".github", "workflows")
		es, err := os.ReadDir(d)
		if err != nil {
			panic(err)
		}
		for _, e := range es {
			ours = append(ours, filepath.Join(d, e.Name()))
		}
	}

	bms := []struct {
		what       string
		files      []string
		shellcheck string
		format     string
	}{
		{
			what:  "minimal",
			files: []string{minimal},
		},
		{
			what:  "minimal",
			files: []string{minimal, minimal, minimal, minimal, minimal},
		},
		{
			what:  "minimal",
			files: []string{minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal},
		},
		{
			what: "minimal",
			files: []string{
				minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal,
				minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal, minimal,
			},
		},
		{
			what:  "small",
			files: []string{small},
		},
		{
			what:  "smalls",
			files: []string{small, small, small},
		},
		{
			what:  "many-smalls",
			files: []string{small, small, small, small, small, small, small, small, small, small},
		},
		{
			what:  "large",
			files: []string{large},
		},
		{
			what:  "larges",
			files: []string{large, large, large},
		},
		{
			what:  "many-larges",
			files: []string{large, large, large, large, large, large, large, large, large, large},
		},
		{
			what:       "small",
			files:      []string{small},
			shellcheck: shellcheck,
		},
		{
			what:       "smalls",
			files:      []string{small, small, small},
			shellcheck: shellcheck,
		},
		{
			what:       "many-smalls",
			files:      []string{small, small, small, small, small, small, small, small, small, small},
			shellcheck: shellcheck,
		},
		{
			what:       "script",
			files:      []string{scripts},
			shellcheck: shellcheck,
		},
		{
			what:       "scripts",
			files:      []string{scripts, scripts, scripts},
			shellcheck: shellcheck,
		},
		{
			what:       "many-scripts",
			files:      []string{scripts, scripts, scripts, scripts, scripts, scripts, scripts, scripts, scripts, scripts},
			shellcheck: shellcheck,
		},
		{
			what:   "many-smalls",
			files:  []string{small, small, small, small, small, small, small, small, small, small},
			format: format,
		},
		{
			what:  "our workflows",
			files: ours,
		},
	}

	for _, bm := range bms {
		sc := ""
		if bm.shellcheck != "" {
			sc = "-tool=shellcheck"
		}
		fm := ""
		if bm.format != "" {
			fm = "-format"
		}
		b.Run(fmt.Sprintf("%s%s%s-%d", bm.what, sc, fm, len(bm.files)), func(b *testing.B) {
			for range b.N {
				opts := workspace.Options{WorkingDir: root, Rules: checking.ConfigurationRules()}
				if bm.what != "our workflows" {
					opts.Config = &repoconfig.Config{}
				}
				session, err := workspace.New(b.Context(), opts)
				if err != nil {
					b.Fatal(err)
				}
				project := proj
				if bm.what == "our workflows" {
					project, err = session.ProjectAt(root)
					if err != nil {
						b.Fatal(err)
					}
				}
				selected, err := session.Files(bm.files, project)
				if err != nil {
					b.Fatal(err)
				}
				result, err := checking.Analyze(b.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: bm.shellcheck}})}, selected.Sources)
				if err != nil {
					b.Fatal(err)
				}

				renderer, err := render.New("", bm.format, false, func() string { return "test" }, nil)
				if err != nil {
					b.Fatal(err)
				}
				if err := renderer.Render(io.Discard, render.FromAnalysis(*result)); err != nil {
					b.Fatal(err)
				}
				if len(result.Diagnostics) > 0 {
					b.Fatal("some error occurred:", result.Diagnostics)
				}
			}
		})
	}
}

func BenchmarkExamplesLintFiles(b *testing.B) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		b.Fatal(err)
	}

	dir := filepath.Join(root, "internal", "development", "tests", "integration", "workflow", "testdata", "examples")
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	proj := workspace.Identity(dir)
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		b.Skipf("shellcheck is not found: %s", err)
	}

	for b.Loop() {
		session, err := workspace.New(b.Context(), workspace.Options{WorkingDir: root, Config: &repoconfig.Config{}, Rules: checking.ConfigurationRules()})
		if err != nil {
			b.Fatal(err)
		}
		selected, err := session.Files(files, proj)
		if err != nil {
			b.Fatal(err)
		}
		result, err := checking.Analyze(b.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: shellcheck}})}, selected.Sources)
		if err != nil {
			b.Fatal(err)
		}

		renderer, err := render.New("", "", false, func() string { return "test" }, nil)
		if err != nil {
			b.Fatal(err)
		}
		if err := renderer.Render(io.Discard, render.FromAnalysis(*result)); err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics) == 0 {
			b.Fatal("no error found")
		}
	}
}

func BenchmarkLintRepository(b *testing.B) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		result, err := checking.Check(b.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Repository})
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Diagnostics) > 0 {
			b.Fatal(result.Diagnostics)
		}
	}
}
