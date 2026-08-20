package actionlint_fuzz

import (
	"testing"

	"github.com/kjanat/actionlint"
	"go.yaml.in/yaml/v4"
)

func canParseByGoYAML(data []byte) (ret bool) {
	ret = true
	defer func() {
		if err := recover(); err != nil {
			ret = false
		}
	}()
	var n yaml.Node
	_ = yaml.Unmarshal(data, &n)
	return
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"))
	f.Add([]byte("on:\n  schedule:\n    - cron: '0 0 * * *'\njobs: {}\n"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if !canParseByGoYAML(data) {
			return
		}

		actionlint.Parse(data)
	})
}
