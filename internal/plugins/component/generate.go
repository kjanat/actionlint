package component

//go:generate go run -C ../../../tools/plugin-contract .

//go:generate go -C ../../../plugins/wit tool wacogo-witgen generate -w kjanat:actionlint/annotated-plugin -o ../../internal/plugins/component/bindings -p actionlint.kjanat.dev/v2/internal/plugins/component/bindings .
// Restore the unchanged host binding's canonical source name after the directory generation.
//go:generate go -C ../../../plugins/wit tool wacogo-witgen generate -w kjanat:actionlint/plugin -o ../../internal/plugins/component/bindings -p actionlint.kjanat.dev/v2/internal/plugins/component/bindings analyzer.wit
// Match dprint's gofumpt 0.0.19 by naming generated files explicitly; directory walks skip them.
//go:generate go run mvdan.cc/gofumpt@v0.12.0 -w bindings/kjanat/actionlint/host/host.go bindings/kjanat/actionlint/host/host.bind.go bindings/kjanat/actionlint/host/host.wrap.go
//go:generate go run mvdan.cc/gofumpt@v0.12.0 -w bindings/kjanat/actionlint/sourceannotations/sourceannotations.go bindings/kjanat/actionlint/sourceannotations/sourceannotations.bind.go bindings/kjanat/actionlint/sourceannotations/sourceannotations.wrap.go
