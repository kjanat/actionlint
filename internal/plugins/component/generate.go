package component

//go:generate go run -C ../../../tools/plugin-contract .

//go:generate go run -C ../../../plugins/wit github.com/partite-ai/wacogo/cmd/wacogo-witgen@v0.0.0-20261016032519-b3984dc3d485 generate -w kjanat:actionlint/plugin -o ../../internal/plugins/component/bindings -p actionlint.kjanat.dev/internal/plugins/component/bindings analyzer.wit
