package scripts

import "testing"

func TestFactRejectsMissingOrMismatchedOccurrence(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"occurrence", "declaration"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			graph, collection := batchFixture(t, false)
			batch, err := NewBatch(graph, collection)
			if err != nil {
				t.Fatal(err)
			}
			if field == "occurrence" {
				batch.data.scripts[0].Occurrence = "unknown"
			} else {
				batch.data.scripts[0].Reference.Node = "another"
			}
			if _, valid := batch.Fact(0, false); valid {
				t.Fatal("invalid occurrence became an entry fact")
			}
		})
	}
}
