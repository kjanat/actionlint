package main

import (
	"strings"
	"testing"

	"go-modules.kjanat.dev/wit"
)

func TestNamedPrimitiveKeepsDefinedTypeIdentity(t *testing.T) {
	resolved, err := wit.DecodeWIT(strings.NewReader(`package test:contract;
interface ids { type occurrence-id = string; }
interface analyzer {
    use ids.{occurrence-id};
    record input { selected: option<list<occurrence-id>> }
    analyze: func(input: input);
}
world plugin { export analyzer; }`))
	if err != nil {
		t.Fatal(err)
	}
	var iface *wit.Interface
	for _, candidate := range resolved.Interfaces {
		if candidate.Match("test:contract/analyzer") {
			iface = candidate
		}
	}
	if iface == nil {
		t.Fatal("analyzer missing")
	}
	g := generator{types: make(map[*wit.TypeDef]string)}
	for name, definition := range iface.TypeDefs.All() {
		value, err := g.value(definition)
		if err != nil {
			t.Fatal(err)
		}
		if name == "occurrence-id" && value != "t0" {
			t.Fatalf("named primitive lacks a defined type index: %s", value)
		}
	}
	generated := g.output.String()
	if strings.Count(generated, "Kind:wp.DefinedKindPrimitive, Primitive:wp.PrimString") != 1 || !strings.Contains(generated, "List:t0") {
		t.Fatalf("nested aliases lost primitive identity:\n%s", generated)
	}
}
