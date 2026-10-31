package actionlint

//go:generate -command generate-config-schema go run ./tools/config
//go:generate generate-config-schema
//go:generate go run ./internal/rulecatalog/generate
//go:generate go run ./internal/expression/context/generate ./internal/expression/context/availability.gen.go
//go:generate node ./internal/webhook/payload/generate/main.mts
//go:generate go run ./internal/webhook/generate --registry ./internal/webhook/webhooks.gen.go
//go:generate go run ./internal/action/generate -runtimes -output internal/action/runtimes.gen.go
//go:generate go run ./internal/action/generate -output internal/action/availability.gen.go
