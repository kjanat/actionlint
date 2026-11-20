package component

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/results"
	"github.com/google/go-cmp/cmp"
	"github.com/kjanat/wacogo"
)

func TestGuestDiagnosticFixRevisions(t *testing.T) {
	t.Parallel()
	const path = "workflow.yml"
	start := sourcetext.Position{Line: 1, Column: 1}
	end := sourcetext.Position{Line: 1, Column: 2}
	digest := strings.Repeat("a", 64)
	for _, test := range []struct {
		name      string
		revisions int
		wantError string
	}{
		{name: "preserves fix", revisions: 1},
		{name: "duplicate revision", revisions: 2, wantError: `duplicate source revision "workflow.yml"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			revisions := make([]wacogo.Val, test.revisions)
			for i := range revisions {
				revisions[i] = wacogo.NewValRecord(field("path", wacogo.ValString(path)), field("digest", wacogo.ValString(digest)))
			}
			edit := wacogo.NewValRecord(
				field("path", wacogo.ValString(path)), field("start", lowerPosition(start)),
				field("end", lowerPosition(end)), field("replacement", wacogo.ValString("x")),
			)
			diagnostic := mixedDiagnostic("replace character", start, end)
			fix := wacogo.NewValRecord(
				field("description", wacogo.ValString("replace character")), field("applicability", wacogo.ValString("safe")),
				field("revisions", wacogo.NewValListOf(revisions...)), field("edits", wacogo.NewValListOf(edit)),
			)
			var fields []wacogo.Field
			for _, name := range []string{"reference", "related", "rule", "code", "severity", "message", "path", "explanation", "help", "documentation-url", "start", "end"} {
				fields = append(fields, field(name, diagnostic.Field(name)))
			}
			fields = append(fields, field("fixes", wacogo.NewValListOf(fix)))
			got, err := liftDiagnostic(wacogo.NewValRecord(fields...))
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || len(got.Fixes) != 0 {
					t.Fatalf("diagnostic = %+v, error = %v, want %q", got, err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []results.Fix{{
				Description: "replace character", Applicability: applicability.Safe, Revisions: map[string]string{path: digest},
				Edits: []results.Edit{{Path: path, Start: start, End: end, Replacement: "x"}},
			}}
			if diff := cmp.Diff(want, got.Fixes); diff != "" {
				t.Fatalf("guest fixes (-want +got):\n%s", diff)
			}
		})
	}
}
