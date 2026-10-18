package actionlint

//go:generate -command generate-config-schema go run ./tools/config
//go:generate generate-config-schema
//go:generate go run ./internal/rulecatalog/generate
//go:generate go run ./internal/expression/context/generate ./internal/expression/context/availability.go
//go:generate go run ./internal/webhook/generate --registry ./internal/webhook/webhooks.go
//go:generate node ./internal/webhook/payload/generate/main.mts
//go:generate go run ./internal/actioncatalog/metadata -runtimes -output internal/action/runtimes_generated.go
//go:generate go run ./internal/actioncatalog/generate -f json ./internal/actioncatalog/popular_actions.json
//go:generate go run ./internal/actioncatalog/metadata -output internal/action/availability_generated.go
