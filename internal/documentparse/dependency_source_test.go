package documentparse

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestDependencySpansArePublishedBeforeTraversal(t *testing.T) {
	for _, action := range []bool{false, true} {
		language := "workflow"
		indent := 6
		prefix := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n"
		address := `jobs["test"].steps`
		if action {
			language, indent = "action", 4
			prefix = "name: fixture\ndescription: fixture\nruns:\n  using: composite\n  steps:\n"
			address = "runs.steps"
		}
		for _, tc := range []struct {
			name, scalar string
			endLine, end int
			block, alias bool
		}{
			{"plain", "owner/action@v1", 6, 24, false, false},
			{"double quoted", `"owner/action@v1"`, 6, 26, false, false},
			{"single quoted", `'owner/action@v1'`, 6, 26, false, false},
			{"escaped quoted", `"owner/\u0061ction@v1"`, 6, 31, false, false},
			{"folded literal expression", `${{ 'owner/action@v1' }}`, 6, 33, false, false},
			{"folded block", ">-", 7, 20, true, false},
			{"literal block", "|-", 7, 20, true, false},
			{"scalar alias", `&shared "owner/action@v1"`, 6, 34, false, true},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				content := prefix + strings.Repeat(" ", indent) + "- uses: " + tc.scalar + "\n"
				if tc.block {
					content += strings.Repeat(" ", indent+4) + "owner/action@v1\n"
				}
				wantCount := 1
				if tc.alias {
					content += strings.Repeat(" ", indent) + "- uses: *shared\n"
					wantCount++
				}
				document := Workflow("fixture.yml", []byte(content))
				if action {
					document = Action("fixture.yml", "fixture.yml", []byte(content))
				}
				if !document.CanAnalyze() || len(document.Findings) != 0 {
					t.Fatalf("invalid dependency fixture: %v", document.Findings)
				}
				snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
				snapshot, err := snapshots.Document("fixture.yml", []byte(content), document)
				if err != nil {
					t.Fatal(err)
				}
				// Consumers can change their decoded input after publication.
				if action {
					document.Metadata.Source[0] = '!'
					document.Metadata.Root.Content = nil
				} else {
					document.Workflow.Jobs = nil
				}
				_, dependencies := snapshot.Declarations()
				if len(dependencies) != wantCount {
					t.Fatalf("dependency count = %d; want %d", len(dependencies), wantCount)
				}
				for i, dependency := range dependencies {
					wantStart := diagnostic.Position{Line: 6, Column: indent + 9}
					wantEnd := diagnostic.Position{Line: tc.endLine, Column: indent + tc.end}
					if dependency.Start != wantStart || dependency.End != wantEnd || dependency.Uses != "owner/action@v1" || dependency.Kind != "action" {
						t.Errorf("dependency span/value = %+v; want %v..%v", dependency, wantStart, wantEnd)
					}
					if dependency.Reference.Address != fmt.Sprintf("%s[%d]", address, i) || dependency.Reference.Document != snapshot.Reference().Document || dependency.Reference.Revision != snapshot.Reference().Revision {
						t.Errorf("dependency lost its source owner: %+v", dependency.Reference)
					}
					if i != 0 && dependency.Reference == dependencies[0].Reference {
						t.Fatal("alias occurrences shared a dependency reference")
					}
				}
			})
		}
	}
}

func TestReusableAndParallelDependencySpans(t *testing.T) {
	content := []byte("on: push\njobs:\n  caller:\n    uses: './.github/workflows/shared.yml'\n  steps:\n    runs-on: ubuntu-latest\n    steps:\n      - parallel:\n          - uses: \"owner/action@v1\"\n")
	document := Workflow("fixture.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("fixture.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	_, dependencies := snapshot.Declarations()
	if len(dependencies) != 2 {
		t.Fatal(dependencies)
	}
	if got := dependencies[0]; got.Kind != "workflow" || got.Start != (diagnostic.Position{Line: 4, Column: 11}) || got.End != (diagnostic.Position{Line: 4, Column: 43}) || got.Reference.Address != `jobs["caller"]` {
		t.Fatalf("reusable call span/owner = %+v", got)
	}
	if got := dependencies[1]; got.Kind != "action" || got.Start != (diagnostic.Position{Line: 9, Column: 19}) || got.End != (diagnostic.Position{Line: 9, Column: 36}) || got.Reference.Address != `jobs["steps"].steps[0].parallel[0]` {
		t.Fatalf("parallel call span/owner = %+v", got)
	}
}

func TestMappingAliasDependencySpans(t *testing.T) {
	for _, action := range []bool{false, true} {
		name := "workflow"
		indent := 6
		prefix := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n"
		address := `jobs["test"].steps`
		if action {
			name, indent = "action", 4
			prefix = "name: fixture\ndescription: fixture\nruns:\n  using: composite\n  steps:\n"
			address = "runs.steps"
		}
		t.Run(name, func(t *testing.T) {
			content := []byte(prefix + strings.Repeat(" ", indent) + "- &step\n" + strings.Repeat(" ", indent+2) + "uses: owner/action@v1\n" + strings.Repeat(" ", indent) + "- *step\n")
			document := Workflow("fixture.yml", content)
			if action {
				document = Action("fixture.yml", "fixture.yml", content)
			}
			if !document.CanAnalyze() || len(document.Findings) != 0 {
				t.Fatal(document.Findings)
			}
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := snapshots.Document("fixture.yml", content, document)
			if err != nil {
				t.Fatal(err)
			}
			_, dependencies := snapshot.Declarations()
			if len(dependencies) != 2 {
				t.Fatal(dependencies)
			}
			for i, dependency := range dependencies {
				if dependency.Start != (diagnostic.Position{Line: 7, Column: indent + 9}) || dependency.End != (diagnostic.Position{Line: 7, Column: indent + 24}) || dependency.Kind != "action" || dependency.Uses != "owner/action@v1" {
					t.Errorf("mapping alias dependency %d span/value: %+v", i, dependency)
				}
				if dependency.Reference.Address != fmt.Sprintf("%s[%d]", address, i) || dependency.Reference.Document != snapshot.Reference().Document || dependency.Reference.Revision != snapshot.Reference().Revision {
					t.Errorf("mapping alias dependency %d lost its owner: %+v", i, dependency.Reference)
				}
			}
			if dependencies[0].Reference.Node == dependencies[1].Reference.Node {
				t.Fatal("mapping alias dependency owners collapsed")
			}
		})
	}
}

func TestFoldedReusableDependencyPhysicalSpan(t *testing.T) {
	content := []byte("on: push\njobs:\n  caller:\n    uses: ${{ './.github/workflows/shared.yml' }}\n")
	document := Workflow("fixture.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("fixture.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	_, dependencies := snapshot.Declarations()
	if len(dependencies) != 1 {
		t.Fatal(dependencies)
	}
	got := dependencies[0]
	if got.Kind != "workflow" || got.Uses != "./.github/workflows/shared.yml" || got.Start != (diagnostic.Position{Line: 4, Column: 11}) || got.End != (diagnostic.Position{Line: 4, Column: 50}) || got.Reference.Address != `jobs["caller"]` || got.Reference.Document != snapshot.Reference().Document || got.Reference.Revision != snapshot.Reference().Revision {
		t.Fatalf("folded reusable dependency span/owner = %+v", got)
	}
}

func TestAliasedParallelDependencySpans(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - &batch\n        parallel:\n          - uses: owner/action@v1\n      - *batch\n")
	document := Workflow("fixture.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("fixture.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	_, dependencies := snapshot.Declarations()
	if len(dependencies) != 2 {
		t.Fatal(dependencies)
	}
	for i, dependency := range dependencies {
		if dependency.Start != (diagnostic.Position{Line: 8, Column: 19}) || dependency.End != (diagnostic.Position{Line: 8, Column: 34}) || dependency.Kind != "action" || dependency.Uses != "owner/action@v1" || dependency.Reference.Address != fmt.Sprintf(`jobs["test"].steps[%d].parallel[0]`, i) || dependency.Reference.Document != snapshot.Reference().Document || dependency.Reference.Revision != snapshot.Reference().Revision {
			t.Errorf("aliased parallel dependency %d span/owner = %+v", i, dependency)
		}
	}
	if dependencies[0].Reference.Node == dependencies[1].Reference.Node {
		t.Fatal("aliased parallel dependency owners collapsed")
	}
}
