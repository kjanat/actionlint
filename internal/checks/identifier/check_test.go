package identifier

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/diagnostic"
	"github.com/google/go-cmp/cmp"
)

func TestValid(t *testing.T) {
	for value, want := range map[string]bool{
		"build": true, "_build": true, "Build-1_2": true,
		"1build": false, "build.1": false, "build 1": false, "": false, "ünïcode": false,
	} {
		if Valid(value) != want {
			t.Errorf("Valid(%q) = %v", value, !want)
		}
	}
}

func TestCheck(t *testing.T) {
	at := func(line int) diagnostic.Position { return diagnostic.Position{Line: line, Column: 5} }
	long := strings.Repeat("x", 100)
	job := Job{
		ID:    &Name{Value: "1build", Start: at(1)},
		Needs: []Name{{Value: "ok", Start: at(2)}, {Value: "bad.need", Start: at(3)}, {Value: "${{ dynamic }}", Dynamic: true, Start: at(4)}},
		Steps: []Name{
			{Value: "first", Start: at(10)},
			{Value: "__reserved", Start: at(11)},
			{Value: long, Start: at(12)},
			{Value: "FIRST", Start: at(13)},
			{Value: "first", Start: at(14)},
			{Value: "bad step", Start: at(15)},
			{Value: "", Start: at(16)},
		},
	}
	want := []diagnostic.Diagnostic{
		{Rule: "id", Start: at(1), Message: `invalid job ID "1build". job ID must start with a letter or _ and contain only alphanumeric characters, -, or _`},
		{Rule: "id", Start: at(3), Message: `invalid job ID "bad.need". job ID must start with a letter or _ and contain only alphanumeric characters, -, or _`},
		{Rule: "id", Start: at(11), Message: `step ID "__reserved" must not start with reserved prefix __`},
		{Rule: "id", Start: at(12), Message: `step ID "` + long + `" must be shorter than 100 characters`},
		{Rule: "id", Start: at(13), Message: `step ID "FIRST" duplicates. previously defined at line:10,col:5. step ID must be unique within a job. note that step ID is case insensitive`},
		{Rule: "id", Start: at(14), Message: `step ID "first" duplicates. previously defined at line:10,col:5. step ID must be unique within a job. note that step ID is case insensitive`},
		{Rule: "id", Start: at(15), Message: `invalid step ID "bad step". step ID must start with a letter or _ and contain only alphanumeric characters, -, or _`},
	}
	if diff := cmp.Diff(want, Check(job)); diff != "" {
		t.Fatal(diff)
	}
	if findings := Check(Job{}); findings != nil {
		t.Fatal(findings)
	}
}
