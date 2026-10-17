package finding

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	workflowlang "actionlint.kjanat.dev/internal/workflow"

	"github.com/google/go-cmp/cmp"
)

func TestErrorErrorAt(t *testing.T) {
	m := "message"
	k := "kind"
	err := At(&workflowlang.Pos{Line: 1, Col: 2}, k, m)
	if err.Message != m {
		t.Errorf("wanted %q but got %q", m, err.Message)
	}
	if err.Path != "" {
		t.Errorf("wanted %q to be empty", err.Path)
	}
	if err.Start.Line != 1 {
		t.Errorf("wanted line 1 but got %d", err.Start.Line)
	}
	if err.Start.Column != 2 {
		t.Errorf("wanted col 2 but got %d", err.Start.Column)
	}
	if err.Rule != k {
		t.Errorf("wanted %q but got %q", k, err.Rule)
	}
}

func TestErrorErrorfAt(t *testing.T) {
	m := "this is message"
	k := "kind"
	err := Errorf(&workflowlang.Pos{Line: 1, Col: 2}, k, "%s is %s", "this", "message")
	if err.Message != m {
		t.Errorf("wanted %q but got %q", m, err.Message)
	}
	if err.Path != "" {
		t.Errorf("wanted %q to be empty", err.Path)
	}
	if err.Start.Line != 1 {
		t.Errorf("wanted line 1 but got %d", err.Start.Line)
	}
	if err.Start.Column != 2 {
		t.Errorf("wanted col 2 but got %d", err.Start.Column)
	}
	if err.Rule != k {
		t.Errorf("wanted %q but got %q", k, err.Rule)
	}
}

func TestErrorSortErrorsByPosition(t *testing.T) {
	testCases := [][]struct {
		line int
		col  int
	}{
		{},
		{
			{1, 2},
		},
		{
			{1, 2},
			{4, 1},
			{3, 20},
			{1, 1},
		},
		{
			{1, 1},
			{1, 1},
			{1, 1},
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			errs := make([]*Finding, 0, len(tc))
			for _, p := range tc {
				errs = append(errs, &Finding{Start: diagmodel.Position{Line: p.line, Column: p.col}})
			}

			slices.SortFunc(errs, Compare)

			for i := range len(errs) - 1 {
				l, r := errs[i], errs[i+1]
				sorted := l.Start.Line <= r.Start.Line
				if l.Start.Line == r.Start.Line {
					sorted = l.Start.Column <= r.Start.Column
				}
				if !sorted {
					t.Fatalf("errs[%d] and errs[%d] are not sorted: %s", i, i+1, errs)
				}
			}
		})
	}
}

func TestErrorSortErrorsByFile(t *testing.T) {
	errs := []*Finding{
		{
			Path: "path/to/C.txt", Start: diagmodel.Position{Line: 1,
				Column: 2},
		},
		{
			Path: "path/to/A.txt", Start: diagmodel.Position{Line: 2,
				Column: 4},
		},
		{
			Path: "path/to/B.txt", Start: diagmodel.Position{Line: 3,
				Column: 6},
		},
	}

	slices.SortFunc(errs, Compare)

	for i, want := range []string{
		"path/to/A.txt",
		"path/to/B.txt",
		"path/to/C.txt",
	} {
		if have := errs[i].Path; have != want {
			t.Errorf("Errors were not sorted correctly. expected %q for errs[%d] but got %q: %v", want, i, have, errs)
		}
	}
}

func TestErrorSortErrorsByMessage(t *testing.T) {
	msgs := []string{}
	for c := 'z'; c >= 'a'; c-- {
		msgs = append(msgs, fmt.Sprintf("Message %c", c))
	}
	for c := 'Z'; c >= 'A'; c-- {
		msgs = append(msgs, fmt.Sprintf("Message %c", c))
	}

	errs := []*Finding{}
	for _, m := range msgs {
		errs = append(errs, &Finding{
			Path: "path/to/file.yaml", Start: diagmodel.Position{Line: 1,
				Column: 1}, Message: m,
		})
	}

	slices.SortFunc(errs, Compare)
	have := []string{}
	for _, e := range errs {
		have = append(have, e.Message)
	}

	sort.Strings(msgs)
	if diff := cmp.Diff(have, msgs); diff != "" {
		t.Fatal(diff)
	}
}

func TestErrorErrorToString(t *testing.T) {
	err := &Finding{
		Message: "this is message", Start: diagmodel.Position{Line: 1,
			Column: 2}, Rule: "test",
	}
	want := ":1:2: this is message [test]"
	have := err.String()
	if want != have {
		t.Fatalf("wanted %q but have %q", want, have)
	}
}
