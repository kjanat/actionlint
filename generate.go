package actionlint

//go:generate -command generate-config-schema go run ./internal/repoconfig/generate
//go:generate generate-config-schema
//go:generate go run ./internal/rulecatalog/generate
//go:generate go run ./internal/workflowcontext/generate ./internal/workflowcontext/availability.go
//go:generate go run ./internal/webhook/generate ./internal/engine/all_webhooks.go
//go:generate go run ./internal/actioncatalog/metadata -runtimes -output internal/engine/action_runtimes.go
//go:generate go run ./internal/actioncatalog/generate -f json ./internal/actioncatalog/popular_actions.json
//go:generate go run ./internal/actioncatalog/metadata -output internal/engine/action_metadata_availability.go
