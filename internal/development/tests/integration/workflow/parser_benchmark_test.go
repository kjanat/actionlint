package workflow_test

import (
	"os"
	"path/filepath"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func BenchmarkParseWorkflow(b *testing.B) {
	type bench struct {
		name  string
		input []byte
	}

	loadBench := func(name string) bench {
		i, err := os.ReadFile(filepath.Join("internal", "development", "tests", "integration", "workflow", "testdata", "bench", name+".yaml"))
		if err != nil {
			b.Fatal(err)
		}
		return bench{name, i}
	}

	for _, bc := range []bench{
		loadBench("minimal"),
		loadBench("small"),
		loadBench("large"),
	} {
		b.Run(bc.name, func(b *testing.B) {
			for range b.N {
				if _, errs := workflowparser.Parse(bc.input); len(errs) > 0 {
					b.Fatal(errs)
				}
			}
		})
	}
}

func BenchmarkParseTestData(b *testing.B) {
	type bench struct {
		name   string
		inputs [][]byte
	}

	loadBench := func(name string) bench {
		inputs := [][]byte{}
		fs, err := filepath.Glob(filepath.Join("internal", "development", "tests", "integration", "workflow", "testdata", name, "*.y*ml"))
		if err != nil {
			b.Fatal(err)
		}
		for _, f := range fs {
			bs, err := os.ReadFile(f)
			if err != nil {
				b.Fatal(err)
			}
			inputs = append(inputs, bs)
		}
		return bench{name, inputs}
	}

	for _, bc := range []bench{
		loadBench("examples"),
		loadBench("ok"),
		loadBench("err"),
	} {
		b.Run(bc.name, func(b *testing.B) {
			for range b.N {
				for _, in := range bc.inputs {
					// Note: Some workflows may cause parse error
					workflowparser.Parse(in)
				}
			}
		})
	}
}
