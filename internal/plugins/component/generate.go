package component

//go:generate go run -C ../../../tools/plugin-contract .

//go:generate go -C ../../../plugins/wit tool wacogo-witgen generate -w kjanat:actionlint/plugin -o ../../internal/plugins/component/bindings -p actionlint.kjanat.dev/internal/plugins/component/bindings analyzer.wit
