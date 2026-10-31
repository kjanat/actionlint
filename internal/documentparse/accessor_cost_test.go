package documentparse

import (
	"strconv"
	"strings"
	"testing"
)

func TestSemanticAccessorsDoNotCopySourceBytes(t *testing.T) {
	for _, size := range []int{32, 1 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			content := []byte("# " + strings.Repeat("x", size) + "\non: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
			document := Workflow("ci.yml", content)
			if !document.CanAnalyze() {
				t.Fatal(document.Findings)
			}
			registry := newCapturedSnapshots(t, t.TempDir())
			snapshot, err := registry.Document("ci.yml", content, document)
			if err != nil {
				t.Fatal(err)
			}
			step := snapshotJobSteps(t, snapshot, "test")[0]
			allocations := testing.AllocsPerRun(100, func() {
				if _, ok := snapshot.WorkflowView(); !ok {
					t.Fatal("workflow is unavailable")
				}
				if err := snapshot.ValidateStep(step); err != nil {
					t.Fatal(err)
				}
				snapshot.Kind()
			})
			if allocations != 0 {
				t.Fatalf("semantic access allocated %v times for %d source bytes", allocations, len(content))
			}
		})
	}
}
