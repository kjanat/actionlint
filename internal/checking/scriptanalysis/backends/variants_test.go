package backends

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation/mask"
	"actionlint.kjanat.dev/v2/internal/externallinters/process"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

// Each variant runs once. Diagnostics map to the templates, merge across
// variants, and name the values of the variants that report them.
func TestVariantRunsMergeFindings(t *testing.T) {
	t.Parallel()
	exclude := "${{ github.ref != 'main' && '--exclude=.[^/]*' || '' }}"
	home := "${{ github.event_name == 'push' && '$HOME' || 'x' }}"
	script := scriptFixture(t, "run: |\n  tar "+exclude+" -cf x.tar .\n  echo "+home+" $1\n")
	at := func(row, column int) sourcetext.Position { return sourcetext.Position{Line: row, Column: column} }
	backend := &reportingBackend{concurrent: true, result: func(request scriptengine.Request) process.DecodedCheck {
		diagnostics := []scriptengine.Diagnostic{{Code: "SC2086", Message: "quote", Location: at(2, strings.Index(request.Source, "$1")-strings.Index(request.Source, "echo")+1)}}
		if strings.Contains(request.Source, "$HOME") {
			diagnostics = append(diagnostics,
				scriptengine.Diagnostic{Code: "SC2035", Message: "glob", Location: at(1, 7), EndLocation: at(1, 9)},
				scriptengine.Diagnostic{Code: "SC2086", Message: "quote", Location: at(2, 6), EndLocation: at(2, 11)},
			)
		}
		return process.DecodedCheck{Diagnostics: diagnostics}
	}}
	prepared := preparation.Prepared{Source: mask.Expressions(script.Text())}
	for _, variant := range mask.Variants(script.Text()) {
		prepared.Variants = append(prepared.Variants, preparation.Variant{Source: variant.Text, Substitutions: variant.Substitutions})
	}
	runner := buildRunner(t, backend, engineOptions{prepare: func(context.Context, scriptanalysis.Unit) (preparation.Prepared, error) { return prepared, nil }})
	findings, err := runner.Analyze(t.Context(), scriptanalysis.Batch{Scripts: []scriptanalysis.Unit{script}})
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.requests) != 2 || backend.requests[0].Source == backend.requests[1].Source || strings.Contains(backend.requests[0].Source+backend.requests[1].Source, "${{") {
		t.Fatalf("requests: %+v", backend.requests)
	}
	type summary struct {
		code, message string
		start, end    sourcetext.Position
	}
	var got []summary
	for _, f := range findings {
		got = append(got, summary{f.Code, f.Message, f.Start, f.End})
	}
	want := []summary{
		{"SC2086", "quote", at(3, 9+len(home)), sourcetext.Position{}},
		{"SC2035", `glob (when ` + exclude + ` is "--exclude=.[^/]*")`, at(2, 7), at(2, 7+len(exclude))},
		{"SC2086", `quote (when ` + home + ` is "$HOME")`, at(3, 8), at(3, 8+len(home))},
	}
	if len(got) != len(want) {
		t.Fatalf("findings %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMergeVariantsDeduplicatesErrorsAndSources(t *testing.T) {
	t.Parallel()
	prepared := preparation.Prepared{Variants: []preparation.Variant{{}, {}}}
	failure := errors.New("tool failed")
	merged := mergeVariants(prepared, []outcome{
		{skipped: true, sources: []string{"a.sh"}, err: failure, copies: map[string]reportedFile{"a.sh": {content: []byte("a")}}},
		{sources: []string{"a.sh", "b.sh"}, err: failure, directory: "/work", copies: map[string]reportedFile{"a.sh": {content: []byte("other")}, "b.sh": {}}},
	})
	if merged.skipped || merged.directory != "/work" || strings.Join(merged.sources, ",") != "a.sh,b.sh" || merged.err.Error() != failure.Error() || string(merged.copies["a.sh"].content) != "a" || len(merged.copies) != 2 {
		t.Fatalf("merged %+v", merged)
	}
	if plain := mergeVariants(prepared, []outcome{{}, {}}); plain.copies != nil {
		t.Fatalf("process outcomes gained copies: %+v", plain)
	}
}
