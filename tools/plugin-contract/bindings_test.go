package main

import (
	"strings"
	"testing"
)

func TestHostTypeDependencyOrder(t *testing.T) {
	input := "\ttypRecord_ := b_.AddType(\"record\", host.List{Elem:typVariant_})\n\t_ = typRecord_\n\ttypVariant_ := b_.AddType(\"variant\", host.String)\n\t_ = typVariant_\n"
	got, err := orderHostTypes([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(got), "typVariant_ :=") > strings.Index(string(got), "typRecord_ :=") {
		t.Fatal(string(got))
	}
	again, err := orderHostTypes(got)
	if err != nil || string(again) != string(got) {
		t.Fatal("ordering is not stable")
	}
}
