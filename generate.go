package actionlint

//go:generate go run ./scripts/generate-config-schema
//go:generate go run ./scripts/generate-availability ./internal/engine/availability.go
//go:generate go run ./scripts/generate-webhook-events ./internal/engine/all_webhooks.go
//go:generate go run ./scripts/generate-action-metadata -runtimes -output internal/engine/action_runtimes.go
//go:generate go run ./scripts/generate-popular-actions -f json ./internal/actioncatalog/popular_actions.json
//go:generate go run ./scripts/generate-action-metadata -output internal/engine/action_metadata_availability.go
