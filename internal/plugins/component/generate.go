package component

//go:generate go run -C ../../../tools/plugin-contract . -bindings
// Match dprint's gofumpt 0.0.19 by naming generated files explicitly; directory walks skip them.
//go:generate go run mvdan.cc/gofumpt@v0.12.0 -w bindings/kjanat/actionlint/host/host.gen.go bindings/kjanat/actionlint/host/host.bind.gen.go bindings/kjanat/actionlint/host/host.wrap.gen.go
//go:generate go run mvdan.cc/gofumpt@v0.12.0 -w bindings/kjanat/actionlint/sourceannotations/sourceannotations.gen.go bindings/kjanat/actionlint/sourceannotations/sourceannotations.bind.gen.go bindings/kjanat/actionlint/sourceannotations/sourceannotations.wrap.gen.go
