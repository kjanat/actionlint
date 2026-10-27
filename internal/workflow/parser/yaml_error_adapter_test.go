package parser

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestYAMLErrorAdapterPreservesLocatedWrappersAndJoinedAdmission(t *testing.T) {
	root, err := codec.Parse([]byte("value: text\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := codec.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	inner := &codec.DecodeError{Errors: []*codec.LoadError{{Message: "first", Mark: codec.Mark{Line: 3, Column: 4}}, {Message: "second", Mark: codec.Mark{Line: 5, Column: 6}}}}
	located := session.Wrap(root, fmt.Errorf("outer: %w", inner))
	want := []*SyntaxError{{Message: "could not parse as YAML: outer: first", Line: 3, Column: 4, Kind: "syntax-check"}, {Message: "could not parse as YAML: outer: second", Line: 5, Column: 6, Kind: "syntax-check"}}
	if got := HandleYAMLUnmarshalError(located); !reflect.DeepEqual(got, want) {
		t.Fatalf("located wrapper context changed: got %v, want %v", got, want)
	}
	if got := HandleYAMLUnmarshalError(fmt.Errorf("outer: %w", inner)); !reflect.DeepEqual(got, want) {
		t.Fatalf("single wrapper lost its diagnostic context: got %v, want %v", got, want)
	}
	admission := codec.NewAdmissionError(&codec.LoadError{Message: "document contains excessive aliasing", Mark: codec.Mark{Line: 7, Column: 8}})
	wantAdmission := &SyntaxError{Message: "could not parse as YAML: document contains excessive aliasing", Line: 7, Column: 8, Kind: "syntax-check"}
	if got := HandleYAMLUnmarshalError(admission); !reflect.DeepEqual(got, []*SyntaxError{wantAdmission}) {
		t.Fatalf("bare admission error changed: %v", got)
	}
	want = append(want, wantAdmission)
	if got := HandleYAMLUnmarshalError(errors.Join(located, admission)); !reflect.DeepEqual(got, want) {
		t.Fatalf("joined errors lost context or order: got %v, want %v", got, want)
	}
}
