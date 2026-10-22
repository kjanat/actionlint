package main

import (
	"bytes"
	"io"
	"maps"
	"os"
	"reflect"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	"actionlint.kjanat.dev/internal/actioncatalog"
)

func TestBundledCatalogRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../../internal/actioncatalog/popular_actions.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := actioncatalog.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	actions := maps.Clone(actioncatalog.PopularActions)
	for ref := range actioncatalog.OutdatedPopularActionSpecs {
		actions[ref] = &actionmeta.Metadata{Runs: actionmeta.Runs{Using: "node12"}}
	}
	var output bytes.Buffer
	if err := newGen(io.Discard, io.Discard, io.Discard).writeCatalog(&output, actions); err != nil {
		t.Fatal(err)
	}
	got, err := actioncatalog.Decode(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("loading and regenerating the bundled catalogue changed its metadata")
	}
}

func TestWriteCatalog(t *testing.T) {
	meta := &actionmeta.Metadata{Name: "Example", Inputs: actionmeta.Inputs{
		"token": {Name: "Token", Required: true}, "old": {Name: "old", Deprecated: true},
	}, Outputs: actionmeta.Outputs{"result": {Name: "Result"}}}
	actions := map[string]*actionmeta.Metadata{"a/b@v1": meta, "a/b@v2": meta, "a/b@v0": {Runs: actionmeta.Runs{Using: "node12"}}}
	g := newGen(io.Discard, io.Discard, io.Discard)
	var out, again bytes.Buffer
	if err := g.writeCatalog(&out, actions); err != nil {
		t.Fatal(err)
	}
	if err := g.writeCatalog(&again, actions); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), again.Bytes()) {
		t.Fatal("catalogue generation is nondeterministic")
	}
	c, err := actioncatalog.Decode(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Definitions) != 1 || c.References["a/b@v1"] != c.References["a/b@v2"] || len(c.Retired) != 1 {
		t.Fatalf("deduplication lost references: %+v", c)
	}
	d := c.Definitions[c.References["a/b@v1"]]
	if len(d.Required) != 1 || d.Required[0] != "token" || len(d.Deprecated) != 1 || d.Inputs[1] != "Token" || d.Outputs[0] != "Result" {
		t.Fatalf("lost sparse metadata: %+v", d)
	}
}
