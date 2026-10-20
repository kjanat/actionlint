package component

//go:generate go run -C ../../../tools/plugin-contract .

//go:generate go -C ../../../plugins/wit tool wacogo-witgen generate -w kjanat:actionlint/plugin -o ../../internal/plugins/component/bindings -p actionlint.kjanat.dev/internal/plugins/component/bindings analyzer.wit
// Keep generated bindings consistent with dprint's gofumpt 0.0.19 plugin.
// Explicit files also format generated code, which gofumpt skips when walking directories.
//go:generate go run mvdan.cc/gofumpt@v0.12.0 -w bindings/kjanat/actionlint/host/host.go bindings/kjanat/actionlint/host/host.bind.go bindings/kjanat/actionlint/host/host.wrap.go
