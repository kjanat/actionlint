package actionlint_fuzz

import (
	"testing"

	"actionlint.kjanat.dev"
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
	f.Add([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`))
	f.Add([]byte(`on:
  schedule:
    - cron: '0 0 * * *'
jobs: {}
`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if !canParseByGoYAML(data) {
			return
		}

		actionlint.Parse(data)
	})
}
