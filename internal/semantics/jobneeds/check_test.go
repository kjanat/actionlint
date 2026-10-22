package jobneeds

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"actionlint.kjanat.dev/diagnostic"
)

func at(line int) diagnostic.Position { return diagnostic.Position{Line: line, Column: 3} }

func job(line int, id string, needs ...string) Job {
	j := Job{ID: Name{Value: id, Start: at(line)}}
	for i, need := range needs {
		j.Needs = append(j.Needs, Name{Value: need, Start: diagnostic.Position{Line: line, Column: 20 + i}})
	}
	return j
}

func messages(findings []diagnostic.Diagnostic) []string {
	result := make([]string, len(findings))
	for i, finding := range findings {
		result[i] = finding.Message
	}
	return result
}

func TestCheck(t *testing.T) {
	for _, test := range []struct {
		name string
		jobs []Job
		want []diagnostic.Diagnostic
	}{
		{"valid graph", []Job{job(1, "build"), job(2, "test", "build"), job(3, "deploy", "Build", "test")}, nil},
		{"missing dependency", []Job{job(1, "test", "build")}, []diagnostic.Diagnostic{
			{Rule: "job-needs", Start: at(1), Message: `job "test" needs job "build" which does not exist in this workflow`},
		}},
		{"duplicate need", []Job{job(1, "build"), job(2, "test", "build", "BUILD")}, []diagnostic.Diagnostic{
			{Rule: "job-needs", Start: diagnostic.Position{Line: 2, Column: 21}, Message: `job ID "BUILD" duplicates in "needs" section. note that job ID is case insensitive`},
		}},
		{"duplicate job", []Job{job(1, "build"), job(2, "BUILD")}, []diagnostic.Diagnostic{
			{Rule: "job-needs", Start: at(2), Message: `job ID "BUILD" duplicates. previously defined at line:1,col:3. note that job ID is case insensitive`},
		}},
		{"self cycle", []Job{job(1, "build", "build")}, []diagnostic.Diagnostic{
			{Rule: "job-needs", Start: at(1), Message: `cyclic dependencies in "needs" job configurations are detected. detected cycle is "build" -> "build"`},
		}},
		{"cycle starts at earliest job", []Job{job(1, "c", "b"), job(2, "a", "c"), job(3, "b", "a")}, []diagnostic.Diagnostic{
			{Rule: "job-needs", Start: at(1), Message: `cyclic dependencies in "needs" job configurations are detected. detected cycle is "c" -> "b" -> "a" -> "c"`},
		}},
		{"missing dependency suppresses cycle detection", []Job{job(1, "a", "b"), job(2, "b", "a", "missing")}, []diagnostic.Diagnostic{
			{Rule: "job-needs", Start: at(2), Message: `job "b" needs job "missing" which does not exist in this workflow`},
		}},
		{"empty ids are skipped", []Job{job(1, ""), job(2, "build", "")}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want, Check(test.jobs)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestCheckReportsOneCycleDeterministically(t *testing.T) {
	jobs := []Job{job(1, "x", "y"), job(2, "y", "x"), job(3, "p", "q"), job(4, "q", "p")}
	first := messages(Check(jobs))
	for range 20 {
		if again := messages(Check(jobs)); len(again) != 1 || again[0] != first[0] {
			t.Fatalf("nondeterministic cycle report: %v vs %v", first, again)
		}
	}
}
