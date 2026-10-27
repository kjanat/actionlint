module actionlint.kjanat.dev/v2

go 1.27.0

toolchain go1.27.3

require (
	github.com/bmatcuk/doublestar/v4 v4.10.3
	github.com/fatih/color v1.19.0
	github.com/google/go-cmp v0.7.0
	github.com/invopop/jsonschema v0.14.0
	github.com/kjanat/wacogo v0.1.2-0.20261025202354-2854ac48d3b7
	github.com/kjanat/wazero v1.11.1-0.20261025184245-2dce1ead9408
	github.com/mattn/go-colorable v0.1.16
	github.com/mattn/go-isatty v0.0.24
	github.com/mattn/go-runewidth v0.0.31
	github.com/mattn/go-shellwords v1.0.16
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/robfig/cron/v3 v3.0.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/yuin/goldmark v1.8.6
	go.yaml.in/yaml/v4 v4.0.0-rc.6
	golang.org/x/mod v0.42.0
	golang.org/x/net v0.61.0
	golang.org/x/sync v0.24.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	golang.org/x/tools v0.51.0
	mvdan.cc/sh/v3 v3.14.1
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pb33f/go-yaml v0.1.1 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.2 // indirect
	go-modules.kjanat.dev v0.8.1-0.20261025190330-2e183b7fc3f2 // indirect
)

tool github.com/kjanat/wacogo/cmd/wacogo-witgen

ignore (
	node_modules
	target
)

replace go.yaml.in/yaml/v4 => github.com/kjanat/go-yaml/v4 v4.0.0-rc.6.0.20261027003936-b29e171e3237
